from __future__ import annotations

import unittest
from typing import Any, cast
from unittest import mock

import grpc

from axern.control.run.v1 import run_pb2, run_pb2_grpc
from axern.control.workspace.v1 import workspace_pb2
from axern_sdk import AsyncAxernClient, AsyncSandbox, AxernClient, Sandbox
from axern_sdk.errors import SandboxConnectionError, SandboxLifecycleError, SandboxRpcError
from fakes import _AsyncFakeClient, _FakeClient


class _RpcFailure(grpc.RpcError):
    def __init__(self, code: grpc.StatusCode) -> None:
        self._code = code

    def code(self):
        return self._code

    def details(self):
        return "inert test transport failure"


class _WorkspaceStub:
    def __init__(self) -> None:
        self.requests = []
        self.failure = False
        self.value = workspace_pb2.Workspace(
            id="ws-retained", node_id="node-a", status=workspace_pb2.WORKSPACE_STATUS_READY,
            capacity_bytes=9007199254741504, inode_limit=9007199254740995,
        )

    def CreateWorkspace(self, request, timeout=None):
        self.requests.append(request)
        if self.failure:
            raise _RpcFailure(grpc.StatusCode.UNAVAILABLE)
        # Serialize the actual generated messages to exercise int64 precision.
        wire = workspace_pb2.Workspace.FromString(self.value.SerializeToString())
        return workspace_pb2.CreateWorkspaceResponse(workspace=wire)

    def GetWorkspace(self, request, timeout=None):
        self.requests.append(request)
        return workspace_pb2.GetWorkspaceResponse(workspace=self.value)

    def ListWorkspaces(self, request, timeout=None):
        self.requests.append(request)
        return workspace_pb2.ListWorkspacesResponse(workspaces=[self.value], next_cursor="next")

    def DeleteWorkspace(self, request, timeout=None):
        self.requests.append(request)
        if self.failure:
            raise _RpcFailure(grpc.StatusCode.UNAVAILABLE)
        return workspace_pb2.DeleteWorkspaceResponse(workspace=workspace_pb2.Workspace(
            id=request.workspace_id, status=workspace_pb2.WORKSPACE_STATUS_DELETING,
        ))


class _AsyncWorkspaceStub(_WorkspaceStub):
    async def CreateWorkspace(self, request, timeout=None):
        return super().CreateWorkspace(request, timeout)

    async def GetWorkspace(self, request, timeout=None):
        return super().GetWorkspace(request, timeout)

    async def ListWorkspaces(self, request, timeout=None):
        return super().ListWorkspaces(request, timeout)

    async def DeleteWorkspace(self, request, timeout=None):
        return super().DeleteWorkspace(request, timeout)


class _RunStub:
    def __init__(self) -> None:
        self.request = None

    def AdmitRun(self, request, timeout=None):
        self.request = request
        return run_pb2.CreateRunResponse(run=run_pb2.Run(id="run-a"))


class _AsyncRunStub(_RunStub):
    async def AdmitRun(self, request, timeout=None):
        return super().AdmitRun(request, timeout)


class _OldRunChannel:
    def __init__(self) -> None:
        self.methods = []
        self.legacy_calls = 0

    def unary_unary(self, method, **_kwargs):
        def invoke(_request, timeout=None):
            self.methods.append(method)
            if method.endswith("/CreateRun"):
                self.legacy_calls += 1
                return run_pb2.CreateRunResponse(run=run_pb2.Run(id="unexpected-ephemeral"))
            raise _RpcFailure(grpc.StatusCode.UNIMPLEMENTED)
        return invoke

    def unary_stream(self, _method, **_kwargs):
        return None


class _OldAsyncRunChannel(_OldRunChannel):
    def unary_unary(self, method, **_kwargs):
        async def invoke(_request, timeout=None):
            self.methods.append(method)
            if method.endswith("/CreateRun"):
                self.legacy_calls += 1
                return run_pb2.CreateRunResponse(run=run_pb2.Run(id="unexpected-ephemeral"))
            raise _RpcFailure(grpc.StatusCode.UNIMPLEMENTED)
        return invoke


class WorkspaceClientTest(unittest.TestCase):
    def test_sync_crud_and_admission_preserve_retained_identity_and_int64(self) -> None:
        client = AxernClient.__new__(AxernClient)
        stub, runs = _WorkspaceStub(), _RunStub()
        cast(Any, client).workspaces = stub
        cast(Any, client).runs = runs
        value = client.create_workspace(node_id="node-a", capacity_bytes=9007199254741504, inode_limit=9007199254740995)
        self.assertEqual(value.capacity_bytes, 9007199254741504)
        self.assertEqual(stub.requests[0].inode_limit, 9007199254740995)
        self.assertEqual(client.get_workspace("ws-retained").id, "ws-retained")
        listing = client.list_workspaces(namespace="team", cursor="current", page_size=7)
        self.assertEqual(listing.next_cursor, "next")
        self.assertEqual((stub.requests[-1].namespace, stub.requests[-1].cursor, stub.requests[-1].page_size), ("team", "current", 7))
        self.assertEqual(client.delete_workspace("ws-retained").status, workspace_pb2.WORKSPACE_STATUS_DELETING)
        client.create_run(environment_id="env-a", workspace_id="ws-retained")
        self.assertEqual(runs.request.workspace_id, "ws-retained")
        stub.failure = True
        count = len(stub.requests)
        with self.assertRaises(SandboxConnectionError):
            client.create_workspace(node_id="node-a", capacity_bytes=4096, inode_limit=64)
        with self.assertRaises(SandboxConnectionError):
            client.delete_workspace("ws-retained")
        self.assertEqual(len(stub.requests), count + 2)

    def test_invalid_capacity_rejected_before_any_rpc(self) -> None:
        client = AxernClient.__new__(AxernClient)
        for capacity in (True, 512.0, 513, 0, -512):
            with self.assertRaises(ValueError):
                client.create_workspace(node_id="node-a", capacity_bytes=capacity, inode_limit=64)

    def test_required_attachment_never_invokes_old_create_run(self) -> None:
        client = AxernClient.__new__(AxernClient)
        channel = _OldRunChannel()
        cast(Any, client).runs = run_pb2_grpc.RunControlStub(cast(Any, channel))
        with self.assertRaises(SandboxRpcError) as caught:
            client.create_run(environment_id="env-a", workspace_id="ws-retained")
        self.assertEqual(caught.exception.code, "UNIMPLEMENTED")
        self.assertEqual(channel.methods, ["/axern.control.run.v1.RunControl/AdmitRun"])
        self.assertEqual(channel.legacy_calls, 0)

    def test_sync_sandbox_close_cancels_writer_without_deleting_workspace(self) -> None:
        client = _FakeClient()
        client.delete_workspace = mock.Mock(side_effect=AssertionError("retained data deletion"))
        with Sandbox(client=client, environment_id="env-a", workspace_id="ws-retained"):
            self.assertEqual(client.created_run["workspace_id"], "ws-retained")
        self.assertEqual(client.cancelled[0][0], "run-1")
        client.delete_workspace.assert_not_called()


    def test_ready_waiter_waits_for_detached_writer_and_checks_identity(self) -> None:
        client = AxernClient.__new__(AxernClient)
        busy = workspace_pb2.Workspace(id="ws-a", status=workspace_pb2.WORKSPACE_STATUS_READY, attached_allocation_id="alloc-a")
        free = workspace_pb2.Workspace(id="ws-a", status=workspace_pb2.WORKSPACE_STATUS_READY)
        cast(Any, client).get_workspace = mock.Mock(side_effect=[busy, free])
        with mock.patch("axern_sdk.client.time.sleep"):
            self.assertFalse(client.wait_workspace_ready("ws-a").attached_allocation_id)
        self.assertEqual(cast(Any, client).get_workspace.call_count, 2)
        cast(Any, client).get_workspace = mock.Mock(return_value=workspace_pb2.Workspace(id="different", status=workspace_pb2.WORKSPACE_STATUS_READY))
        with self.assertRaises(SandboxLifecycleError):
            client.wait_workspace_ready("ws-a")


class AsyncWorkspaceClientTest(unittest.IsolatedAsyncioTestCase):
    async def test_async_crud_admission_and_mutations_are_not_retried(self) -> None:
        client = AsyncAxernClient.__new__(AsyncAxernClient)
        stub, runs = _AsyncWorkspaceStub(), _AsyncRunStub()
        raw_client = cast(Any, client)
        raw_client._channel = object()
        raw_client._loop = None
        raw_client._environments = object()
        raw_client._tunnels = object()
        raw_client._workspaces = stub
        raw_client._runs = runs
        value = await client.create_workspace(node_id="node-a", capacity_bytes=9007199254741504, inode_limit=9007199254740995)
        self.assertEqual(value.inode_limit, 9007199254740995)
        self.assertEqual(stub.requests[0].capacity_bytes, 9007199254741504)
        self.assertEqual((await client.get_workspace("ws-retained")).id, "ws-retained")
        self.assertEqual((await client.list_workspaces(namespace="team", cursor="current", page_size=7)).next_cursor, "next")
        self.assertEqual(stub.requests[-1].namespace, "team")
        self.assertEqual((await client.delete_workspace("ws-retained")).status, workspace_pb2.WORKSPACE_STATUS_DELETING)
        await client.create_run(environment_id="env-a", workspace_id="ws-retained")
        self.assertEqual(runs.request.workspace_id, "ws-retained")
        stub.failure = True
        count = len(stub.requests)
        with self.assertRaises(SandboxConnectionError):
            await client.create_workspace(node_id="node-a", capacity_bytes=4096, inode_limit=64)
        with self.assertRaises(SandboxConnectionError):
            await client.delete_workspace("ws-retained")
        self.assertEqual(len(stub.requests), count + 2)

    async def test_async_invalid_capacity_rejected_before_any_rpc(self) -> None:
        client = AsyncAxernClient.__new__(AsyncAxernClient)
        for capacity in (True, 512.0, 513, 0, -512):
            with self.assertRaises(ValueError):
                await client.create_workspace(node_id="node-a", capacity_bytes=capacity, inode_limit=64)

    async def test_async_sandbox_close_retains_workspace(self) -> None:
        client = _AsyncFakeClient()
        client.delete_workspace = mock.AsyncMock(side_effect=AssertionError("retained data deletion"))
        async with AsyncSandbox(client=client, environment_id="env-a", workspace_id="ws-retained"):
            self.assertEqual(client.created_run["workspace_id"], "ws-retained")
        self.assertEqual(client.cancelled[0][0], "run-1")
        client.delete_workspace.assert_not_called()


    async def test_async_old_wire_cannot_drop_required_workspace(self) -> None:
        client = AsyncAxernClient.__new__(AsyncAxernClient)
        channel = _OldAsyncRunChannel()
        raw = cast(Any, client)
        raw._channel, raw._loop = object(), None
        raw._environments, raw._tunnels = object(), object()
        raw._runs = run_pb2_grpc.RunControlStub(cast(Any, channel))
        with self.assertRaises(SandboxRpcError):
            await client.create_run(environment_id="env-a", workspace_id="ws-retained")
        self.assertEqual(channel.methods, ["/axern.control.run.v1.RunControl/AdmitRun"])
        self.assertEqual(channel.legacy_calls, 0)

    async def test_async_ready_waiter_observes_writer_cleanup(self) -> None:
        client = AsyncAxernClient.__new__(AsyncAxernClient)
        busy = workspace_pb2.Workspace(id="ws-a", status=workspace_pb2.WORKSPACE_STATUS_READY, attached_allocation_id="alloc-a")
        free = workspace_pb2.Workspace(id="ws-a", status=workspace_pb2.WORKSPACE_STATUS_READY)
        cast(Any, client).get_workspace = mock.AsyncMock(side_effect=[busy, free])
        with mock.patch("axern_sdk.async_client.asyncio.sleep", new=mock.AsyncMock()):
            self.assertFalse((await client.wait_workspace_ready("ws-a")).attached_allocation_id)
        self.assertEqual(cast(Any, client).get_workspace.await_count, 2)
        cast(Any, client).get_workspace = mock.AsyncMock(return_value=workspace_pb2.Workspace(id="different", status=workspace_pb2.WORKSPACE_STATUS_READY))
        with self.assertRaises(SandboxLifecycleError):
            await client.wait_workspace_ready("ws-a")


if __name__ == "__main__":
    unittest.main()
