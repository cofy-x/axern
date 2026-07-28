from __future__ import annotations

import asyncio
import unittest
from types import SimpleNamespace

from axern_sdk import (
    AsyncSandbox,
    ExecResult,
    ExecStreamEvent,
    Sandbox,
    SandboxConnectionError,
    SandboxExecError,
    SandboxNotStartedError,
)
from fakes import _AsyncFakeClient, _FakeClient



class SandboxTest(unittest.TestCase):

    def test_exec_uses_active_sandbox_allocation(self) -> None:
        client = _FakeClient()
        calls = []

        class FakeNodeClient:
            def __init__(self, **kwargs) -> None:
                calls.append(kwargs)

            def exec(self, argv, **kwargs):
                calls.append({"argv": argv, **kwargs})
                return ExecResult(exit_code=0, stdout=b"ok\n")

        with Sandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            _node_client_factory=FakeNodeClient,
        ) as sandbox:
            result = sandbox.exec(["python", "-V"], check=True)

        self.assertEqual(result.stdout_text(), "ok\n")
        self.assertEqual(calls[0]["allocation_id"], "alloc-1")
        self.assertEqual(calls[1]["argv"], ["python", "-V"])
        self.assertTrue(calls[1]["check"])

    def test_exec_text_mode_decodes_output(self) -> None:
        client = _FakeClient()

        class FakeNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            def exec(self, argv, **kwargs):
                self.argv = argv
                self.kwargs = kwargs
                return ExecResult(exit_code=0, stdout="olá\n", stderr="")

        with Sandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            _node_client_factory=FakeNodeClient,
        ) as sandbox:
            result = sandbox.exec("printf olá", text=True, encoding="utf-8")

        self.assertEqual(result.stdout, "olá\n")
        self.assertEqual(result.stdout_text(), "olá\n")

    def test_exec_requires_started_sandbox(self) -> None:
        sandbox = Sandbox(client=_FakeClient(), image="docker.io/library/python:3.12-slim")
        with self.assertRaises(SandboxNotStartedError):
            sandbox.exec(["true"])

    def test_exec_stream_proxies_node_events(self) -> None:
        client = _FakeClient()

        class FakeNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            def exec_stream(self, argv, **kwargs):
                yield ExecStreamEvent(stream="stdout", data=b"hello")
                yield ExecStreamEvent(stream="exit", exit_code=0)

        with Sandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            _node_client_factory=FakeNodeClient,
        ) as sandbox:
            events = list(sandbox.exec_stream(["printf", "hello"]))

        self.assertEqual(events[0].text(), "hello")
        self.assertEqual(events[1].exit_code, 0)

    def test_sync_stream_result_requires_exit_event(self) -> None:
        from axern_sdk.node import NodeSandboxClient

        class FakeProcess:
            def write(self, data):
                del data

            def close_stdin(self):
                pass

            def close(self):
                pass

            def events(self):
                yield ExecStreamEvent(stream="stdout", data=b"partial")

        class FakeNodeClient(NodeSandboxClient):
            def process(self, *args, **kwargs):
                del args, kwargs
                return FakeProcess()

        client = FakeNodeClient.__new__(FakeNodeClient)
        with self.assertRaises(SandboxConnectionError):
            client._exec_via_process(
                ["echo", "partial"],
                env=None,
                cwd="",
                timeout_seconds=0,
                user="",
                tty=False,
                input=b"",
                lease_ttl_seconds=60,
                rpc_timeout=None,
            )

    def test_sync_exec_with_stdin_preserves_tty_flag(self) -> None:
        from axern_sdk.node import NodeSandboxClient

        class FakeProcess:
            def write(self, data):
                del data

            def close_stdin(self):
                pass

            def close(self):
                pass

            def events(self):
                yield ExecStreamEvent(stream="exit", exit_code=0)

        class FakeNodeClient(NodeSandboxClient):
            def process(self, *args, **kwargs):
                del args
                self.seen_tty = kwargs["tty"]
                return FakeProcess()

        client = FakeNodeClient(client=SimpleNamespace(), allocation_id="alloc-1")
        client.exec(["cat"], input=b"payload")

        self.assertFalse(client.seen_tty)

    def test_sync_exec_accepts_shell_command_string(self) -> None:
        from axern_sdk.node import NodeSandboxClient

        class FakeProcess:
            def write(self, data):
                del data

            def close_stdin(self):
                pass

            def close(self):
                pass

            def events(self):
                yield ExecStreamEvent(stream="exit", exit_code=0)

        class FakeNodeClient(NodeSandboxClient):
            def process(self, argv, **kwargs):
                del kwargs
                self.seen_argv = argv
                return FakeProcess()

        client = FakeNodeClient(client=SimpleNamespace(), allocation_id="alloc-1")
        client.exec("printf hello")

        self.assertEqual(client.seen_argv, ["/bin/sh", "-lc", "printf hello"])

    def test_sync_node_exec_text_mode_decodes_result_and_input(self) -> None:
        from axern_sdk.node import NodeSandboxClient

        class FakeProcess:
            def __init__(self) -> None:
                self.seen_input = b""

            def write(self, data):
                self.seen_input += data

            def close_stdin(self):
                pass

            def close(self):
                pass

            def events(self):
                yield ExecStreamEvent(stream="stdout", data=self.seen_input)
                yield ExecStreamEvent(stream="exit", exit_code=0)

        class FakeNodeClient(NodeSandboxClient):
            def process(self, *args, **kwargs):
                del args, kwargs
                self.process_instance = FakeProcess()
                return self.process_instance

        client = FakeNodeClient(client=SimpleNamespace(), allocation_id="alloc-1")
        result = client.exec("cat", input="olá", text=True, encoding="utf-8")

        self.assertEqual(client.process_instance.seen_input, "olá".encode())
        self.assertEqual(result.stdout, "olá")

    def test_checked_exec_error_exposes_result(self) -> None:
        result = ExecResult(exit_code=2, stderr=b"bad")
        err = SandboxExecError(argv=["false"], result=result)
        self.assertIs(err.result, result)

    def test_exec_result_raise_for_status(self) -> None:
        ExecResult(exit_code=0).raise_for_status(["true"])
        with self.assertRaises(SandboxExecError):
            ExecResult(exit_code=7, stderr="bad").raise_for_status(["false"])

    def test_exec_result_text_and_bytes_helpers(self) -> None:
        byte_result = ExecResult(exit_code=0, stdout="é".encode(), stderr=b"")
        text_result = ExecResult(exit_code=0, stdout="é", stderr="")

        self.assertEqual(byte_result.stdout_text(), "é")
        self.assertEqual(text_result.stdout_text(), "é")
        self.assertEqual(text_result.stdout_bytes(), "é".encode())



class AsyncSandboxTest(unittest.IsolatedAsyncioTestCase):

    async def test_async_exec_text_mode_decodes_output(self) -> None:
        client = _AsyncFakeClient()

        class FakeAsyncNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            async def exec(self, argv, **kwargs):
                del argv, kwargs
                return ExecResult(exit_code=0, stdout="async-ok\n")

        async with AsyncSandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            _node_client_factory=FakeAsyncNodeClient,
        ) as sandbox:
            result = await sandbox.exec("printf async-ok", text=True)

        self.assertEqual(result.stdout, "async-ok\n")

    async def test_async_exec_stream_proxies_node_events(self) -> None:
        client = _AsyncFakeClient()

        class FakeAsyncNodeClient:
            def __init__(self, **kwargs) -> None:
                del kwargs

            async def exec_stream(self, argv, **kwargs):
                yield ExecStreamEvent(stream="stdout", data=b"hello")
                yield ExecStreamEvent(stream="exit", exit_code=0)

        async with AsyncSandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            _node_client_factory=FakeAsyncNodeClient,
        ) as sandbox:
            events = [event async for event in sandbox.exec_stream(["printf", "hello"])]

        self.assertEqual(events[0].text(), "hello")
        self.assertEqual(events[1].exit_code, 0)

    async def test_async_stream_result_requires_exit_event(self) -> None:
        from axern_sdk.node import AsyncNodeSandboxClient

        class FakeProcess:
            async def write(self, data):
                del data

            async def close_stdin(self):
                pass

            async def close(self):
                pass

            async def events(self):
                yield ExecStreamEvent(stream="stdout", data=b"partial")

        class FakeNodeClient(AsyncNodeSandboxClient):
            async def process(self, *args, **kwargs):
                del args, kwargs
                return FakeProcess()

        client = FakeNodeClient.__new__(FakeNodeClient)
        with self.assertRaises(SandboxConnectionError):
            await client._exec_via_process(
                ["echo", "partial"],
                env=None,
                cwd="",
                timeout_seconds=0,
                user="",
                tty=False,
                input=b"",
                lease_ttl_seconds=60,
                rpc_timeout=None,
            )

    async def test_async_exec_with_stdin_preserves_tty_flag(self) -> None:
        from axern_sdk.node import AsyncNodeSandboxClient

        class FakeProcess:
            async def write(self, data):
                del data

            async def close_stdin(self):
                pass

            async def close(self):
                pass

            async def events(self):
                yield ExecStreamEvent(stream="exit", exit_code=0)

        class FakeNodeClient(AsyncNodeSandboxClient):
            async def process(self, *args, **kwargs):
                del args
                self.seen_tty = kwargs["tty"]
                return FakeProcess()

        client = FakeNodeClient(client=SimpleNamespace(), allocation_id="alloc-1")
        await client.exec(["cat"], input=b"payload")

        self.assertFalse(client.seen_tty)

    async def test_async_exec_accepts_shell_command_string(self) -> None:
        from axern_sdk.node import AsyncNodeSandboxClient

        class FakeProcess:
            async def write(self, data):
                del data

            async def close_stdin(self):
                pass

            async def close(self):
                pass

            async def events(self):
                yield ExecStreamEvent(stream="exit", exit_code=0)

        class FakeNodeClient(AsyncNodeSandboxClient):
            async def process(self, argv, **kwargs):
                del kwargs
                self.seen_argv = argv
                return FakeProcess()

        client = FakeNodeClient(client=SimpleNamespace(), allocation_id="alloc-1")
        await client.exec("printf hello")

        self.assertEqual(client.seen_argv, ["/bin/sh", "-lc", "printf hello"])

    async def test_async_node_exec_text_mode_decodes_result_and_input(self) -> None:
        from axern_sdk.node import AsyncNodeSandboxClient

        class FakeProcess:
            def __init__(self) -> None:
                self.seen_input = b""

            async def write(self, data):
                self.seen_input += data

            async def close_stdin(self):
                pass

            async def close(self):
                pass

            async def events(self):
                await asyncio.sleep(0)
                yield ExecStreamEvent(stream="stdout", data=self.seen_input)
                yield ExecStreamEvent(stream="exit", exit_code=0)

        class FakeNodeClient(AsyncNodeSandboxClient):
            async def process(self, *args, **kwargs):
                del args, kwargs
                self.process_instance = FakeProcess()
                return self.process_instance

        client = FakeNodeClient(client=SimpleNamespace(), allocation_id="alloc-1")
        result = await client.exec("cat", input="olá", text=True, encoding="utf-8")

        self.assertEqual(client.process_instance.seen_input, "olá".encode())
        self.assertEqual(result.stdout, "olá")

    async def test_async_exec_requires_started_sandbox(self) -> None:
        sandbox = AsyncSandbox(client=_AsyncFakeClient(), image="docker.io/library/python:3.12-slim")
        with self.assertRaises(SandboxNotStartedError):
            await sandbox.exec(["true"])



if __name__ == "__main__":
    unittest.main()
