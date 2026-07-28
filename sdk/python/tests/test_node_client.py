from __future__ import annotations

import queue
import unittest
from unittest.mock import patch

import grpc

from axern.node.sandbox.v1 import node_pb2
from axern_sdk import (
    SandboxCancelledError,
    SandboxCapabilityErrorInfo,
    SandboxConnectionError,
    SandboxNotFoundError,
    SandboxPreconditionError,
    SandboxRpcError,
    SandboxTimeoutError,
    sandbox_capability_error_info,
)
from axern_sdk._internal.errors import sandbox_rpc_error


class _GatewayClient:
    def __init__(self) -> None:
        self._channel = object()


class _ClosableChannel:
    def __init__(self, *, fail_close: bool = False) -> None:
        self.closed = False
        self._fail_close = fail_close

    def close(self) -> None:
        self.closed = True
        if self._fail_close:
            raise RuntimeError("close failed")


class _AsyncClosableChannel:
    def __init__(self, *, fail_close: bool = False) -> None:
        self.closed = False
        self._fail_close = fail_close

    async def close(self) -> None:
        self.closed = True
        if self._fail_close:
            raise RuntimeError("close failed")


class _AsyncProcessCall:
    def __init__(self, *responses: object, fail_done: bool = False) -> None:
        self.done = False
        self.writes: list[object] = []
        self._responses = list(responses)
        self._fail_done = fail_done

    async def write(self, request) -> None:
        self.writes.append(request)

    async def read(self):
        if self._responses:
            return self._responses.pop(0)
        return grpc.aio.EOF

    async def done_writing(self) -> None:
        self.done = True
        if self._fail_done:
            raise RuntimeError("done failed")


class SandboxTest(unittest.TestCase):
    def test_sync_node_file_browser_computer_and_capability_use_gateway_allocation_request(self) -> None:
        from axern_sdk.node import NodeSandboxClient
        from axern_sdk.node import client as node_client_module

        calls: list[tuple[str, object, float | None]] = []

        class FakeStub:
            def __init__(self, channel) -> None:
                self.channel = channel

            def ReadFile(self, request, timeout=None):
                calls.append(("read", request, timeout))
                return node_pb2.ReadFileResponse(data=b"hello")

            def WriteFile(self, request, timeout=None):
                calls.append(("write", request, timeout))
                return node_pb2.WriteFileResponse()

            def BrowserOpen(self, request, timeout=None):
                calls.append(("browser_open", request, timeout))
                return node_pb2.BrowserStatusResponse(available=True, running=True, pid=88, url=request.url)

            def ComputerUseScreenshot(self, request, timeout=None):
                calls.append(("computer_screenshot", request, timeout))
                return node_pb2.ComputerUseScreenshotResponse(data=b"png", content_type="image/png")

            def CapabilityStatus(self, request, timeout=None):
                calls.append(("capability_status", request, timeout))
                return node_pb2.CapabilityStatusResponse(ready=True, capabilities=["browser"])

        client = NodeSandboxClient(client=_GatewayClient(), allocation_id="alloc-1")
        with patch.object(node_client_module.node_pb2_grpc, "NodeSandboxStub", FakeStub):
            self.assertEqual(client.read_file("/tmp/out.txt", rpc_timeout=3), b"hello")
            client.write_file("/tmp/out.txt", b"hello", create_parents=False, rpc_timeout=3)
            browser = client.browser_open("data:text/html,open", rpc_timeout=3)
            screenshot = client.computer_use_screenshot(rpc_timeout=3)
            capability = client.capability_status(rpc_timeout=3)

        self.assertTrue(browser.running)
        self.assertEqual(screenshot.data, b"png")
        self.assertTrue(capability.ready)
        self.assertEqual([name for name, _, _ in calls], ["read", "write", "browser_open", "computer_screenshot", "capability_status"])
        for _, request, timeout in calls:
            self.assertEqual(request.allocation_id, "alloc-1")
            self.assertEqual(getattr(request, "attempt", 0), 0)
            self.assertEqual(getattr(request, "execution_lease_token", ""), "")
            self.assertEqual(timeout, 3)

    def test_sync_process_and_exec_use_gateway_process_rpc(self) -> None:
        from axern_sdk.node import NodeSandboxClient
        from axern_sdk.node import client as node_client_module

        open_requests: list[object] = []

        class FakeStub:
            def __init__(self, channel) -> None:
                del channel

            def Process(self, requests, timeout=None):
                del timeout
                first = next(requests)
                open_requests.append(first.open)
                return iter(
                    [
                        node_pb2.ProcessResponse(ready=node_pb2.ProcessReady()),
                        node_pb2.ProcessResponse(stdout=b"hello"),
                        node_pb2.ProcessResponse(stderr=b"warn"),
                        node_pb2.ProcessResponse(exit=node_pb2.ExecExit(exit_code=0)),
                    ]
                )

        client = NodeSandboxClient(client=_GatewayClient(), allocation_id="alloc-1")
        with patch.object(node_client_module.node_pb2_grpc, "NodeSandboxStub", FakeStub):
            result = client.exec(["/bin/echo", "hello"])

        self.assertEqual(result.exit_code, 0)
        self.assertEqual(result.stdout, b"hello")
        self.assertEqual(result.stderr, b"warn")
        self.assertEqual(open_requests[0].allocation_id, "alloc-1")
        self.assertEqual(open_requests[0].attempt, 0)
        self.assertEqual(open_requests[0].execution_lease_token, "")
        self.assertEqual(list(open_requests[0].spec.argv), ["/bin/echo", "hello"])

    def test_sync_exec_image_uses_gateway_unary_rpc(self) -> None:
        from axern_sdk.node import ImageProcessMount, NodeSandboxClient
        from axern_sdk.node import client as node_client_module

        calls = []

        class FakeStub:
            def __init__(self, channel) -> None:
                del channel

            def ExecImage(self, request, timeout=None):
                calls.append((request, timeout))
                return node_pb2.ExecImageResponse(exit_code=0, stdout=b"ok")

        client = NodeSandboxClient(client=_GatewayClient(), allocation_id="alloc-1")
        with patch.object(node_client_module.node_pb2_grpc, "NodeSandboxStub", FakeStub):
            result = client.exec_image(
                "alpine:latest",
                ["echo", "ok"],
                mounts=[ImageProcessMount(sandbox_path="/workspace", target_path="/mnt", readonly=True)],
                rpc_timeout=5,
            )

        self.assertEqual(result.stdout, b"ok")
        request, timeout = calls[0]
        self.assertEqual(timeout, 5)
        self.assertEqual(request.allocation_id, "alloc-1")
        self.assertEqual(request.attempt, 0)
        self.assertEqual(request.execution_lease_token, "")
        self.assertEqual(request.spec.mounts[0].target_path, "/mnt")

    def test_sync_archive_methods_use_gateway_streams(self) -> None:
        from axern_sdk.node import NodeSandboxClient
        from axern_sdk.node import client as node_client_module

        upload_requests = []
        download_requests = []

        class FakeStub:
            def __init__(self, channel) -> None:
                del channel

            def UploadArchive(self, requests, timeout=None):
                del timeout
                upload_requests.extend(list(requests))
                return node_pb2.UploadArchiveResponse()

            def DownloadArchive(self, request, timeout=None):
                del timeout
                download_requests.append(request)
                return iter([node_pb2.DownloadArchiveResponse(chunk=b"a"), node_pb2.DownloadArchiveResponse(chunk=b"b")])

        client = NodeSandboxClient(client=_GatewayClient(), allocation_id="alloc-1")
        out = bytearray()
        with patch.object(node_client_module.node_pb2_grpc, "NodeSandboxStub", FakeStub):
            client.upload_archive("/workspace", lambda: iter([b"tar"]))
            client.download_archive("/workspace", out.extend)

        self.assertEqual(upload_requests[0].open.allocation_id, "alloc-1")
        self.assertEqual(upload_requests[0].open.attempt, 0)
        self.assertEqual(upload_requests[0].open.execution_lease_token, "")
        self.assertEqual(upload_requests[1].chunk, b"tar")
        self.assertEqual(download_requests[0].allocation_id, "alloc-1")
        self.assertEqual(download_requests[0].attempt, 0)
        self.assertEqual(download_requests[0].execution_lease_token, "")
        self.assertEqual(bytes(out), b"ab")

    def test_sync_process_replays_prefetched_output_before_exit(self) -> None:
        from axern_sdk.node import SandboxProcess

        channel = _ClosableChannel()
        process = SandboxProcess(
            channel=channel,
            responses=iter([node_pb2.ProcessResponse(exit=node_pb2.ExecExit(exit_code=7, message="done"))]),
            requests=queue.Queue(),
            prefetched=[node_pb2.ProcessResponse(stdout=b"prefetched")],
        )
        events = list(process.events())
        self.assertEqual(events[0].data, b"prefetched")
        self.assertEqual(events[1].exit_code, 7)
        self.assertTrue(channel.closed)

    def test_sync_process_preserves_no_exit_error_when_cleanup_fails(self) -> None:
        from axern_sdk.node import SandboxProcess

        process = SandboxProcess(channel=_ClosableChannel(fail_close=True), responses=iter([]), requests=queue.Queue())
        with self.assertRaisesRegex(SandboxConnectionError, "without exit status"):
            list(process.events())

    def test_direct_node_config_is_not_public_api(self) -> None:
        import axern_sdk
        import axern_sdk.node

        legacy_name = "Node" + "ConnectionConfig"
        self.assertFalse(hasattr(axern_sdk, legacy_name))
        self.assertFalse(hasattr(axern_sdk.node, legacy_name))

    def test_sandbox_rpc_errors_are_mapped(self) -> None:
        class FakeRpcError(grpc.RpcError):
            def __init__(self, code, details: str = "boom"):
                self._code = code
                self._details = details

            def code(self):
                return self._code

            def details(self):
                return self._details

        timeout = sandbox_rpc_error(FakeRpcError(grpc.StatusCode.DEADLINE_EXCEEDED), operation="sandbox exec", allocation_id="alloc-1")
        not_found = sandbox_rpc_error(FakeRpcError(grpc.StatusCode.NOT_FOUND), operation="sandbox read file", allocation_id="alloc-1")
        failed_precondition = sandbox_rpc_error(FakeRpcError(grpc.StatusCode.FAILED_PRECONDITION), operation="sandbox list directory")
        invalid_argument = sandbox_rpc_error(FakeRpcError(grpc.StatusCode.INVALID_ARGUMENT), operation="sandbox exec")
        unavailable = sandbox_rpc_error(FakeRpcError(grpc.StatusCode.UNAVAILABLE), operation="sandbox exec")
        cancelled = sandbox_rpc_error(FakeRpcError(grpc.StatusCode.CANCELLED), operation="sandbox exec")
        capability = sandbox_rpc_error(
            FakeRpcError(
                grpc.StatusCode.FAILED_PRECONDITION,
                "sandboxd browser status failed: sandboxd /browser/status returned status 503 "
                "(unavailable): browser crashed; sandboxd user process state=running; "
                "providers 1/1 available; browser provider degraded: browser crashed; "
                "missing dependencies: chromium (not found)",
            ),
            operation="sandbox browser status",
        )

        self.assertIsInstance(timeout, SandboxTimeoutError)
        self.assertEqual(timeout.operation, "sandbox exec")
        self.assertEqual(timeout.allocation_id, "alloc-1")
        self.assertTrue(timeout.retryable)
        self.assertIsInstance(not_found, SandboxNotFoundError)
        self.assertIsInstance(not_found, SandboxRpcError)
        self.assertEqual(not_found.code, "NOT_FOUND")
        self.assertFalse(not_found.retryable)
        self.assertIsInstance(failed_precondition, SandboxPreconditionError)
        self.assertIsInstance(invalid_argument, SandboxRpcError)
        self.assertIsInstance(unavailable, SandboxConnectionError)
        self.assertTrue(unavailable.retryable)
        self.assertIsInstance(cancelled, SandboxCancelledError)
        assert isinstance(capability, SandboxPreconditionError)
        self.assertEqual(
            capability.capability,
            SandboxCapabilityErrorInfo(
                capability="browser",
                provider="browser",
                provider_state="degraded",
                reason="browser crashed",
                missing_dependencies=("chromium (not found)",),
            ),
        )
        self.assertEqual(sandbox_capability_error_info("plain failure"), None)


class AsyncSandboxTest(unittest.IsolatedAsyncioTestCase):
    async def test_async_node_file_browser_computer_and_capability_use_gateway_allocation_request(self) -> None:
        from axern_sdk.node import AsyncNodeSandboxClient
        from axern_sdk.node import async_client as node_client_module

        calls: list[tuple[str, object, float | None]] = []

        class FakeStub:
            def __init__(self, channel) -> None:
                self.channel = channel

            async def ReadFile(self, request, timeout=None):
                calls.append(("read", request, timeout))
                return node_pb2.ReadFileResponse(data=b"hello")

            async def BrowserOpen(self, request, timeout=None):
                calls.append(("browser_open", request, timeout))
                return node_pb2.BrowserStatusResponse(available=True, running=True, pid=88, url=request.url)

            async def ComputerUseScreenshot(self, request, timeout=None):
                calls.append(("computer_screenshot", request, timeout))
                return node_pb2.ComputerUseScreenshotResponse(data=b"png", content_type="image/png")

            async def CapabilityStatus(self, request, timeout=None):
                calls.append(("capability_status", request, timeout))
                return node_pb2.CapabilityStatusResponse(ready=True, capabilities=["browser"])

        client = AsyncNodeSandboxClient(client=_GatewayClient(), allocation_id="alloc-1")
        with patch.object(node_client_module.node_pb2_grpc, "NodeSandboxStub", FakeStub):
            self.assertEqual(await client.read_file("/tmp/out.txt", rpc_timeout=3), b"hello")
            browser = await client.browser_open("data:text/html,open", rpc_timeout=3)
            screenshot = await client.computer_use_screenshot(rpc_timeout=3)
            capability = await client.capability_status(rpc_timeout=3)

        self.assertTrue(browser.running)
        self.assertEqual(screenshot.data, b"png")
        self.assertTrue(capability.ready)
        for _, request, timeout in calls:
            self.assertEqual(request.allocation_id, "alloc-1")
            self.assertEqual(getattr(request, "attempt", 0), 0)
            self.assertEqual(getattr(request, "execution_lease_token", ""), "")
            self.assertEqual(timeout, 3)

    async def test_async_process_and_exec_use_gateway_process_rpc(self) -> None:
        from axern_sdk.node import AsyncNodeSandboxClient
        from axern_sdk.node import async_client as node_client_module

        calls: list[_AsyncProcessCall] = []

        class FakeStub:
            def __init__(self, channel) -> None:
                del channel

            def Process(self, timeout=None):
                del timeout
                call = _AsyncProcessCall(
                    node_pb2.ProcessResponse(ready=node_pb2.ProcessReady()),
                    node_pb2.ProcessResponse(stdout=b"hello"),
                    node_pb2.ProcessResponse(stderr=b"warn"),
                    node_pb2.ProcessResponse(exit=node_pb2.ExecExit(exit_code=0)),
                )
                calls.append(call)
                return call

        client = AsyncNodeSandboxClient(client=_GatewayClient(), allocation_id="alloc-1")
        with patch.object(node_client_module.node_pb2_grpc, "NodeSandboxStub", FakeStub):
            result = await client.exec(["/bin/echo", "hello"])

        self.assertEqual(result.exit_code, 0)
        self.assertEqual(result.stdout, b"hello")
        self.assertEqual(result.stderr, b"warn")
        open_request = calls[0].writes[0].open
        self.assertEqual(open_request.allocation_id, "alloc-1")
        self.assertEqual(open_request.attempt, 0)
        self.assertEqual(open_request.execution_lease_token, "")
        self.assertEqual(list(open_request.spec.argv), ["/bin/echo", "hello"])
        self.assertTrue(calls[0].done)

    async def test_async_archive_methods_use_gateway_streams(self) -> None:
        from axern_sdk.node import AsyncNodeSandboxClient
        from axern_sdk.node import async_client as node_client_module

        upload_requests = []
        download_requests = []

        async def download_stream():
            yield node_pb2.DownloadArchiveResponse(chunk=b"a")
            yield node_pb2.DownloadArchiveResponse(chunk=b"b")

        class FakeStub:
            def __init__(self, channel) -> None:
                del channel

            async def UploadArchive(self, requests, timeout=None):
                del timeout
                async for request in requests:
                    upload_requests.append(request)
                return node_pb2.UploadArchiveResponse()

            def DownloadArchive(self, request, timeout=None):
                del timeout
                download_requests.append(request)
                return download_stream()

        client = AsyncNodeSandboxClient(client=_GatewayClient(), allocation_id="alloc-1")
        out = bytearray()
        with patch.object(node_client_module.node_pb2_grpc, "NodeSandboxStub", FakeStub):
            await client.upload_archive("/workspace", lambda: iter([b"tar"]))
            await client.download_archive("/workspace", out.extend)

        self.assertEqual(upload_requests[0].open.allocation_id, "alloc-1")
        self.assertEqual(upload_requests[0].open.attempt, 0)
        self.assertEqual(upload_requests[0].open.execution_lease_token, "")
        self.assertEqual(upload_requests[1].chunk, b"tar")
        self.assertEqual(download_requests[0].allocation_id, "alloc-1")
        self.assertEqual(download_requests[0].attempt, 0)
        self.assertEqual(download_requests[0].execution_lease_token, "")
        self.assertEqual(bytes(out), b"ab")

    async def test_async_process_replays_prefetched_output_before_exit(self) -> None:
        from axern_sdk.node import AsyncSandboxProcess

        channel = _AsyncClosableChannel()
        process = AsyncSandboxProcess(
            channel=channel,
            call=_AsyncProcessCall(node_pb2.ProcessResponse(exit=node_pb2.ExecExit(exit_code=7, message="done"))),
            prefetched=[node_pb2.ProcessResponse(stdout=b"prefetched")],
        )
        events = [event async for event in process.events()]
        self.assertEqual(events[0].data, b"prefetched")
        self.assertEqual(events[1].exit_code, 7)
        self.assertTrue(channel.closed)

    async def test_async_process_preserves_no_exit_error_when_cleanup_fails(self) -> None:
        from axern_sdk.node import AsyncSandboxProcess

        process = AsyncSandboxProcess(channel=_AsyncClosableChannel(fail_close=True), call=_AsyncProcessCall())
        with self.assertRaisesRegex(SandboxConnectionError, "without exit status"):
            [event async for event in process.events()]


if __name__ == "__main__":
    unittest.main()
