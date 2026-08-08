from __future__ import annotations

import asyncio
import os
import time
import unittest
from unittest import mock

import grpc

from axern.control.service.v1 import service_pb2, service_types_pb2
from axern_sdk import (
    AsyncAxernClient,
    AsyncSandbox,
    AxernClient,
    CapabilityStatus,
    ExecResult,
    HTTPProbe,
    Sandbox,
    SandboxNotStartedError,
    ServiceProbe,
    TCPProbe,
    VolumeMount,
)
import axern_sdk.client as client_module
from axern_sdk.client import _resource_spec
from fakes import _AsyncFakeClient, _FakeClient, _FakeConnector



class SandboxTest(unittest.TestCase):

    def test_service_backed_sandbox_opens_tunnel_and_cleans_up(self) -> None:
        client = _FakeClient()
        connectors: list[_FakeConnector] = []

        def connector_factory(**kwargs):
            connector = _FakeConnector(**kwargs)
            connectors.append(connector)
            return connector

        with Sandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            registry_credential_id="sec-regcred",
            request_cpu="2",
            request_memory="4GiB",
            request_writable_layer="6GiB",
            limit_cpu="4",
            limit_memory="8GiB",
            limit_writable_layer="10GiB",
            upstream="127.0.0.1:8080",
            remote_port=8786,
            _connector_factory=connector_factory,
            _renew_interval_seconds=0.01,
        ) as sandbox:
            self.assertEqual(sandbox.environment_id, "env-1")
            self.assertEqual(sandbox.service_id, "svc-1")
            self.assertEqual(sandbox.allocation_id, "alloc-1")
            self.assertEqual(sandbox.attempt, 7)
            self.assertEqual(sandbox.bound_addr, "127.0.0.1:8786")
            self.assertEqual(client.created_environment["image_ref"], "docker.io/library/python:3.12-slim")
            self.assertEqual(client.created_environment["registry_credential_id"], "sec-regcred")
            self.assertEqual(client.created_service["request_cpu"], "2")
            self.assertEqual(client.created_service["request_memory"], "4GiB")
            self.assertEqual(client.created_service["request_writable_layer"], "6GiB")
            self.assertEqual(client.created_service["limit_cpu"], "4")
            self.assertEqual(client.created_service["limit_memory"], "8GiB")
            self.assertEqual(client.created_service["limit_writable_layer"], "10GiB")
            self.assertEqual(client.created_tunnel["allocation_id"], "alloc-1")
            self.assertEqual(client.created_tunnel["local_target"], "127.0.0.1:8080")
            self.assertEqual(client.created_tunnel["remote_port"], 8786)
            self.assertTrue(connectors[0].started)
            deadline = time.monotonic() + 1
            while not client.renewed and time.monotonic() < deadline:
                time.sleep(0.01)
            self.assertEqual(client.renewed[0][0], "tun-1")
            self.assertEqual(client.renewed[0][1], "client-token")

        self.assertTrue(connectors[0].stopped)
        self.assertEqual(client.revoked[0][0], "tun-1")
        self.assertEqual(client.deleted[0][0], "svc-1")
        self.assertEqual(client.purged, [])
        self.assertEqual(client.deleted_environments[0][0], "env-1")
        self.assertNotIn("template_id", client.created_environment)

    def test_requires_exactly_one_environment_source(self) -> None:
        client = _FakeClient()
        with self.assertRaises(ValueError):
            Sandbox(client=client, image="image", template_id="python311")
        with self.assertRaises(ValueError):
            Sandbox(client=client)

    def test_sandbox_passes_volume_mounts_to_service(self) -> None:
        client = _FakeClient()
        volumes = [
            VolumeMount("data", "/data"),
            VolumeMount(" cache ", " /cache ", readonly=True, options=("rbind", " nodev ")),
        ]

        with Sandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            volumes=volumes,
        ):
            pass

        self.assertEqual(client.created_service["volume_mounts"], tuple(volumes))
        self.assertEqual(volumes[1].name, "cache")
        self.assertEqual(volumes[1].target, "/cache")
        self.assertEqual(volumes[1].options, ("rbind", "nodev"))

    def test_create_service_builds_volume_mount_protos(self) -> None:
        class ServiceStub:
            request = None

            def CreateService(self, request, timeout=None):
                del timeout
                self.request = request
                return service_pb2.CreateServiceResponse(service=service_types_pb2.Service(id="svc-1"))

        client = AxernClient.__new__(AxernClient)
        stub = ServiceStub()
        client.services = stub

        service = client.create_service(
            environment_id="env-1",
            volume_mounts=[
                VolumeMount("data", "/data"),
                VolumeMount("cache", "/cache", readonly=True, options=("rbind",)),
            ],
        )

        self.assertEqual(service.id, "svc-1")
        mounts = list(stub.request.config.volume_mounts)
        self.assertEqual(len(mounts), 2)
        self.assertEqual(mounts[0].name, "data")
        self.assertEqual(mounts[0].target, "/data")
        self.assertFalse(mounts[0].readonly)
        self.assertEqual(mounts[1].name, "cache")
        self.assertEqual(mounts[1].target, "/cache")
        self.assertTrue(mounts[1].readonly)
        self.assertEqual(list(mounts[1].options), ["rbind"])

    def test_create_service_builds_node_selector(self) -> None:
        class ServiceStub:
            request = None

            def CreateService(self, request, timeout=None):
                del timeout
                self.request = request
                return service_pb2.CreateServiceResponse(service=service_types_pb2.Service(id="svc-1"))

        client = AxernClient.__new__(AxernClient)
        stub = ServiceStub()
        client.services = stub

        client.create_service(
            environment_id="env-1",
            node_selector={"kubernetes.io/hostname": "node-a"},
        )

        self.assertEqual(
            dict(stub.request.config.placement.node_selector),
            {"kubernetes.io/hostname": "node-a"},
        )

    def test_create_service_builds_probe_protos(self) -> None:
        class ServiceStub:
            request = None

            def CreateService(self, request, timeout=None):
                del timeout
                self.request = request
                return service_pb2.CreateServiceResponse(service=service_types_pb2.Service(id="svc-1"))

        client = AxernClient.__new__(AxernClient)
        stub = ServiceStub()
        client.services = stub

        client.create_service(
            environment_id="env-1",
            readiness_probe=ServiceProbe(
                http=HTTPProbe(port=8080, path="/readyz"),
                period=0.1,
                timeout=1.0,
            ),
            liveness_probe=ServiceProbe(tcp=TCPProbe(port=8080), failure_threshold=3),
        )

        self.assertEqual(stub.request.readiness_probe.http.port, 8080)
        self.assertEqual(stub.request.readiness_probe.http.path, "/readyz")
        self.assertEqual(
            stub.request.readiness_probe.http.scheme,
            service_types_pb2.HTTP_PROBE_SCHEME_HTTP,
        )
        self.assertEqual(stub.request.readiness_probe.period.ToMilliseconds(), 100)
        self.assertEqual(stub.request.readiness_probe.timeout.ToMilliseconds(), 1000)
        self.assertEqual(stub.request.liveness_probe.tcp.port, 8080)
        self.assertEqual(stub.request.liveness_probe.failure_threshold, 3)

    def test_service_probe_rejects_submillisecond_and_negative_values(self) -> None:
        for kwargs in (
            {"period": 0.0001},
            {"timeout": float("inf")},
            {"success_threshold": 1.5},
            {"success_threshold": -1},
            {"failure_threshold": -1},
        ):
            with self.subTest(kwargs=kwargs):
                with self.assertRaises(ValueError):
                    ServiceProbe(http=HTTPProbe(port=8080), **kwargs)

    def test_client_from_env_reads_control_settings(self) -> None:
        fake_channel = grpc.insecure_channel("127.0.0.1:9")
        env = {
            "AXERN_ENDPOINT": "127.0.0.1:24099",
            "AXERN_TLS_CA_CERT": "/tmp/ca.crt",
            "AXERN_TLS_CERT": "/tmp/client.crt",
            "AXERN_TLS_KEY": "/tmp/client.key",
        }
        with mock.patch.dict(os.environ, env), mock.patch.object(
            client_module,
            "control_channel",
            return_value=fake_channel,
        ) as channel_factory:
            client = AxernClient.from_env()

        channel_factory.assert_called_once_with(
            "127.0.0.1:24099",
            tls_ca_cert="/tmp/ca.crt",
            tls_cert="/tmp/client.crt",
            tls_key="/tmp/client.key",
            tls_server_name="",
            proxy_mode="env",
        )
        client.close()

    def test_create_environment_rejects_image_with_explicit_template(self) -> None:
        client = AxernClient.__new__(AxernClient)
        with self.assertRaises(ValueError):
            client.create_environment(image_ref="docker.io/library/python:3.12-slim", template_id="python311")

    def test_create_environment_requires_template_or_image(self) -> None:
        client = AxernClient.__new__(AxernClient)
        with self.assertRaises(ValueError):
            client.create_environment()

    def test_resource_spec_rejects_negative_values(self) -> None:
        for kwargs in (
            {"request_cpu": "-1"},
            {"request_memory": "-1"},
            {"request_writable_layer": "-1"},
            {"limit_cpu": "-1"},
            {"limit_memory": "-1"},
            {"limit_writable_layer": "-1"},
        ):
            with self.subTest(kwargs=kwargs):
                with self.assertRaises(ValueError):
                    _resource_spec(**kwargs)

    def test_resource_spec_parses_friendly_values(self) -> None:
        resources = _resource_spec(
            request_cpu=0.5,
            request_memory="128Mi",
            request_writable_layer="256Mi",
            limit_cpu="1.5",
            limit_memory="1Gi",
            limit_writable_layer="2Gi",
        )
        self.assertIsNotNone(resources)
        assert resources is not None
        self.assertEqual(resources.requests.cpu_milli, 500)
        self.assertEqual(resources.requests.memory_bytes, 128 * 1024 * 1024)
        self.assertEqual(resources.requests.writable_layer_bytes, 256 * 1024 * 1024)
        self.assertEqual(resources.limits.cpu_milli, 1500)
        self.assertEqual(resources.limits.memory_bytes, 1024 * 1024 * 1024)
        self.assertEqual(resources.limits.writable_layer_bytes, 2 * 1024 * 1024 * 1024)

    def test_close_skips_purge_when_delete_service_fails(self) -> None:
        client = _FakeClient()

        def fail_delete_service(service_id: str, **kwargs):
            del service_id, kwargs
            raise RuntimeError("control plane unavailable")

        sandbox = Sandbox(client=client, image="docker.io/library/python:3.12-slim")
        sandbox.start()
        client.delete_service = fail_delete_service
        sandbox.close()

        self.assertEqual(client.purged, [])
        self.assertEqual(client.deleted_environments[0][0], "env-1")

    def test_capability_status_uses_node_client(self) -> None:
        client = _FakeClient()
        calls = []

        class FakeNodeClient:
            def __init__(self, **kwargs) -> None:
                calls.append(("init", kwargs))

            def capability_status(self, **kwargs):
                calls.append(("capability_status", kwargs))
                return CapabilityStatus(ready=True, capabilities=("health", "file"))

        with Sandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            _node_client_factory=FakeNodeClient,
        ) as sandbox:
            status = sandbox.capability_status(timeout_seconds=7)

        self.assertTrue(status.ready)
        self.assertEqual(status.capabilities, ("health", "file"))
        self.assertEqual(calls[0][1]["allocation_id"], "alloc-1")
        self.assertEqual(calls[1], ("capability_status", {"rpc_timeout": 7}))

    def test_async_client_can_be_constructed_before_event_loop(self) -> None:
        client = AsyncAxernClient("127.0.0.1:9")

        async def run() -> None:
            try:
                with self.assertRaises(grpc.aio.AioRpcError):
                    await client.list_service_replicas("svc-1", timeout=0.01)
            finally:
                await client.close()

        asyncio.run(run())



class AsyncSandboxTest(unittest.IsolatedAsyncioTestCase):

    async def test_async_sandbox_exec_and_cleanup(self) -> None:
        client = _AsyncFakeClient()
        calls = []

        class FakeAsyncNodeClient:
            def __init__(self, **kwargs) -> None:
                calls.append(kwargs)

            async def exec(self, argv, **kwargs):
                calls.append({"argv": argv, **kwargs})
                return ExecResult(exit_code=0, stdout=b"async-ok\n")

        async with AsyncSandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            registry_credential_id="sec-regcred",
            _node_client_factory=FakeAsyncNodeClient,
        ) as sandbox:
            result = await sandbox.exec(["python", "-V"], check=True)
            self.assertEqual(sandbox.allocation_id, "alloc-1")
            self.assertEqual(sandbox.attempt, 7)

        self.assertEqual(result.stdout_text(), "async-ok\n")
        self.assertEqual(client.created_environment["registry_credential_id"], "sec-regcred")
        self.assertEqual(calls[0]["allocation_id"], "alloc-1")
        self.assertEqual(calls[1]["argv"], ["python", "-V"])
        self.assertEqual(client.deleted[0][0], "svc-1")
        self.assertEqual(client.deleted_environments[0][0], "env-1")

    async def test_async_sandbox_passes_volume_mounts_to_service(self) -> None:
        client = _AsyncFakeClient()
        volumes = [VolumeMount("data", "/data", readonly=True)]

        async with AsyncSandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            volumes=volumes,
        ):
            pass

        self.assertEqual(client.created_service["volume_mounts"], tuple(volumes))

    async def test_async_capability_status_uses_node_client(self) -> None:
        client = _AsyncFakeClient()
        calls = []

        class FakeAsyncNodeClient:
            def __init__(self, **kwargs) -> None:
                calls.append(("init", kwargs))

            async def capability_status(self, **kwargs):
                calls.append(("capability_status", kwargs))
                return CapabilityStatus(ready=True, capabilities=("health", "file"))

        async with AsyncSandbox(
            client=client,
            image="docker.io/library/python:3.12-slim",
            _node_client_factory=FakeAsyncNodeClient,
        ) as sandbox:
            status = await sandbox.capability_status(timeout_seconds=7)

        self.assertTrue(status.ready)
        self.assertEqual(status.capabilities, ("health", "file"))
        self.assertEqual(calls[0][1]["allocation_id"], "alloc-1")
        self.assertEqual(calls[1], ("capability_status", {"rpc_timeout": 7}))

    async def test_start_cancellation_cleans_created_resources(self) -> None:
        class SlowReplicaClient(_AsyncFakeClient):
            def __init__(self) -> None:
                super().__init__()
                self.replica_wait_started = asyncio.Event()

            async def list_service_replicas(self, service_id: str, **kwargs):
                del service_id, kwargs
                self.replica_wait_started.set()
                await asyncio.sleep(3600)
                return []

        client = SlowReplicaClient()
        sandbox = AsyncSandbox(client=client, image="docker.io/library/python:3.12-slim")
        task = asyncio.create_task(sandbox.start())
        await asyncio.wait_for(client.replica_wait_started.wait(), timeout=1.0)

        task.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await task

        self.assertEqual(client.deleted[0][0], "svc-1")
        self.assertEqual(client.deleted_environments[0][0], "env-1")
        with self.assertRaises(SandboxNotStartedError):
            _ = sandbox.state



if __name__ == "__main__":
    unittest.main()
