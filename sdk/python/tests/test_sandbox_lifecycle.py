from __future__ import annotations

import asyncio
import os
import time
import unittest
from unittest import mock

import grpc

from axern_sdk import (
    AsyncAxernClient,
    AsyncSandbox,
    AxernClient,
    CapabilityStatus,
    DeclaredOutput,
    DeclaredOutputFormat,
    ExecResult,
    ImageMount,
    Sandbox,
    SandboxConnectionError,
    SandboxNotStartedError,
    SandboxLifecycleError,
    SandboxNotFoundError,
    SecretEnvVar,
    SecretFile,
)
import axern_sdk.client as client_module
from axern_sdk.client import _resource_spec
from fakes import _AsyncFakeClient, _FakeClient, _FakeConnector


class SandboxTest(unittest.TestCase):
    def test_run_backed_sandbox_opens_tunnel_and_cleans_up(self) -> None:
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
            request_ephemeral_storage="6GiB",
            limit_cpu="4",
            limit_memory="8GiB",
            limit_ephemeral_storage="10GiB",
            declared_outputs=[
                DeclaredOutput(
                    "/tmp/result.json",
                    DeclaredOutputFormat.FILE,
                    "application/json",
                )
            ],
            image_mounts=[ImageMount("registry.example/tool@sha256:aaa", "/__tool")],
            secret_env=[SecretEnvVar("WORKLOAD_TOKEN", "secret-workload", "token")],
            secret_files=[
                SecretFile("/run/secrets/config", "secret-config", "config.json")
            ],
            upstream="127.0.0.1:8080",
            remote_port=8786,
            _connector_factory=connector_factory,
            _renew_interval_seconds=0.01,
        ) as sandbox:
            self.assertEqual(sandbox.environment_id, "env-1")
            self.assertEqual(sandbox.run_id, "run-1")
            self.assertEqual(sandbox.allocation_id, "alloc-1")
            self.assertEqual(sandbox.bound_addr, "127.0.0.1:8786")
            self.assertEqual(
                client.created_environment["image_ref"],
                "docker.io/library/python:3.12-slim",
            )
            self.assertEqual(
                client.created_environment["registry_credential_id"], "sec-regcred"
            )
            self.assertEqual(client.created_run["request_cpu"], "2")
            self.assertEqual(client.created_run["request_memory"], "4GiB")
            self.assertEqual(client.created_run["request_ephemeral_storage"], "6GiB")
            self.assertEqual(client.created_run["limit_cpu"], "4")
            self.assertEqual(client.created_run["limit_memory"], "8GiB")
            self.assertEqual(client.created_run["limit_ephemeral_storage"], "10GiB")
            self.assertEqual(
                client.created_run["declared_outputs"][0].path, "/tmp/result.json"
            )
            self.assertEqual(client.created_run["image_mounts"][0].target, "/__tool")
            self.assertEqual(client.created_run["secret_env"][0].name, "WORKLOAD_TOKEN")
            self.assertEqual(
                client.created_run["secret_files"][0].path,
                "/run/secrets/config",
            )
            self.assertEqual(client.created_tunnel["allocation_id"], "alloc-1")
            self.assertEqual(client.created_tunnel["remote_port"], 8786)
            self.assertTrue(connectors[0].started)
            deadline = time.monotonic() + 1
            while not client.renewed and time.monotonic() < deadline:
                time.sleep(0.01)
            self.assertEqual(client.renewed[0][0], "tun-1")
            self.assertEqual(client.renewed[0][1], "client-token")

        self.assertTrue(connectors[0].stopped)
        self.assertEqual(client.revoked[0][0], "tun-1")
        self.assertEqual(client.cancelled[0][0], "run-1")
        self.assertEqual(client.deleted_environments[0][0], "env-1")

    def test_requires_environment_source(self) -> None:
        client = _FakeClient()
        with self.assertRaises(ValueError):
            Sandbox(client=client)

    def test_run_projections_are_frozen_per_sandbox_and_not_inherited(self) -> None:
        client = _FakeClient()
        mounts = [ImageMount("registry.example/tool@sha256:aaa", "/__tool")]
        secret_env = [SecretEnvVar("WORKLOAD_TOKEN", "secret-workload", "token")]
        first = Sandbox(
            client=client,
            environment_id="env-1",
            image_mounts=mounts,
            secret_env=secret_env,
        )
        mounts.append(ImageMount("registry.example/late@sha256:bbb", "/__late"))
        secret_env.clear()
        with first:
            pass
        first_spec = client.created_run
        self.assertEqual(len(first_spec["image_mounts"]), 1)
        self.assertEqual(len(first_spec["secret_env"]), 1)

        with Sandbox(client=client, environment_id="env-1"):
            pass
        self.assertEqual(client.created_run["image_mounts"], [])
        self.assertEqual(client.created_run["secret_env"], [])
        self.assertEqual(client.created_run["secret_files"], [])

    def test_client_from_env_reads_control_settings(self) -> None:
        fake_channel = grpc.insecure_channel("127.0.0.1:9")
        env = {
            "AXERN_ENDPOINT": "127.0.0.1:24099",
            "AXERN_TLS_CA_CERT": "/tmp/ca.crt",
            "AXERN_TLS_CERT": "/tmp/client.crt",
            "AXERN_TLS_KEY": "/tmp/client.key",
        }
        with (
            mock.patch.dict(os.environ, env),
            mock.patch.object(
                client_module,
                "control_channel",
                return_value=fake_channel,
            ) as channel_factory,
        ):
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

    def test_create_environment_requires_image(self) -> None:
        client = AxernClient.__new__(AxernClient)
        with self.assertRaises(ValueError):
            client.create_environment()

    def test_resource_spec_rejects_negative_values(self) -> None:
        for kwargs in (
            {"request_cpu": "-1"},
            {"request_memory": "-1"},
            {"request_ephemeral_storage": "-1"},
            {"limit_cpu": "-1"},
            {"limit_memory": "-1"},
            {"limit_ephemeral_storage": "-1"},
        ):
            with self.subTest(kwargs=kwargs):
                with self.assertRaises(ValueError):
                    _resource_spec(**kwargs)

    def test_resource_spec_parses_friendly_values(self) -> None:
        resources = _resource_spec(
            request_cpu=0.5,
            request_memory="128Mi",
            request_ephemeral_storage="256Mi",
            limit_cpu="1.5",
            limit_memory="1Gi",
            limit_ephemeral_storage="2Gi",
        )
        self.assertIsNotNone(resources)
        assert resources is not None
        self.assertEqual(resources.requests.cpu_milli, 500)
        self.assertEqual(resources.requests.memory_bytes, 128 * 1024 * 1024)
        self.assertEqual(resources.requests.ephemeral_storage_bytes, 256 * 1024 * 1024)
        self.assertEqual(resources.limits.cpu_milli, 1500)
        self.assertEqual(resources.limits.memory_bytes, 1024 * 1024 * 1024)
        self.assertEqual(
            resources.limits.ephemeral_storage_bytes, 2 * 1024 * 1024 * 1024
        )

    def test_interrupted_context_entry_cleans_acknowledged_resources(self) -> None:
        for interruption in (KeyboardInterrupt(), SystemExit(7)):
            with self.subTest(interruption=type(interruption)):
                client = _FakeClient()
                sandbox = Sandbox(client=client, image="docker.io/library/python:3.12-slim")
                with mock.patch.object(client, "watch_run", side_effect=interruption) as watch:
                    with self.assertRaises(type(interruption)) as caught:
                        with sandbox:
                            self.fail("interrupted Sandbox entered the context body")

                self.assertIs(caught.exception, interruption)
                self.assertEqual(watch.call_args.args, ("run-1",))
                self.assertEqual(client.cancelled, [("run-1", {"timeout": 30.0})])
                self.assertEqual(client.deleted_environments, [("env-1", {"timeout": 30.0})])

    def test_failed_start_preserves_errors_and_retries_the_same_run(self) -> None:
        rpc_error = SandboxConnectionError(
            operation="cancel run", code="UNAVAILABLE", details="control plane unavailable"
        )
        for start_error, cleanup_error, group_type in (
            (KeyboardInterrupt(), rpc_error, BaseExceptionGroup),
            (RuntimeError("readiness failed"), rpc_error, ExceptionGroup),
            (KeyboardInterrupt(), SystemExit(7), BaseExceptionGroup),
        ):
            with self.subTest(start_error=type(start_error), cleanup_error=type(cleanup_error)):
                client = _FakeClient()
                sandbox = Sandbox(client=client, image="docker.io/library/python:3.12-slim")
                cancel = mock.Mock(side_effect=[cleanup_error, None])
                with (
                    mock.patch.object(client, "watch_run", side_effect=start_error),
                    mock.patch.object(client, "cancel_run", cancel),
                ):
                    with self.assertRaises(BaseException) as caught:
                        sandbox.start()
                    group = caught.exception
                    self.assertIs(type(group), group_type)
                    assert isinstance(group, BaseExceptionGroup)
                    self.assertIs(group.exceptions[0], start_error)
                    release_failure = group.exceptions[1]
                    if isinstance(cleanup_error, Exception):
                        assert isinstance(release_failure, ExceptionGroup)
                        self.assertEqual(release_failure.exceptions, (cleanup_error,))
                    else:
                        self.assertIs(release_failure, cleanup_error)
                    with self.assertRaises(SandboxNotStartedError):
                        _ = sandbox.state
                    with self.assertRaisesRegex(SandboxLifecycleError, "cleanup is pending"):
                        sandbox.start()
                    sandbox.close()
                    sandbox.close()

                self.assertEqual(cancel.call_args_list, [mock.call("run-1", timeout=30.0)] * 2)
                self.assertEqual(client.deleted_environments, [("env-1", {"timeout": 30.0})])

    def test_close_still_deletes_environment_when_cancel_run_fails(self) -> None:
        client = _FakeClient()

        cancel = mock.Mock(side_effect=[RuntimeError("control plane unavailable"), None])
        sandbox = Sandbox(client=client, image="docker.io/library/python:3.12-slim")
        sandbox.start()
        with mock.patch.object(client, "cancel_run", cancel):
            with self.assertRaisesRegex(ExceptionGroup, "sandbox cleanup failed"):
                sandbox.close()
            with self.assertRaises(SandboxNotStartedError):
                _ = sandbox.state
            with self.assertRaisesRegex(SandboxLifecycleError, "cleanup is pending"):
                sandbox.start()
            sandbox.close()
            sandbox.close()

        self.assertEqual(cancel.call_args_list, [mock.call("run-1", timeout=30.0)] * 2)
        self.assertEqual(len(client.deleted_environments), 1)

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
                with self.assertRaises(SandboxConnectionError):
                    await client.cancel_run("run-1", timeout=0.01)
            finally:
                await client.close()

        asyncio.run(run())


class AsyncSandboxTest(unittest.IsolatedAsyncioTestCase):
    async def test_close_recovers_lost_environment_delete_acknowledgement(self) -> None:
        client = _AsyncFakeClient()
        sandbox = AsyncSandbox(client=client, image="python")
        await sandbox.start()
        absent = SandboxNotFoundError(
            operation="delete environment", code="NOT_FOUND", details="resource not found"
        )
        delete = mock.AsyncMock(side_effect=[RuntimeError("connection lost"), absent])
        with mock.patch.object(client, "delete_environment", delete):
            with self.assertRaises(ExceptionGroup):
                await sandbox.close()
            with self.assertRaisesRegex(SandboxLifecycleError, "cleanup is pending"):
                await sandbox.start()
            await asyncio.gather(sandbox.close(), sandbox.close())

        self.assertEqual(delete.await_args_list, [mock.call("env-1", timeout=30.0)] * 2)
        self.assertEqual(len(client.cancelled), 1)

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
            image_mounts=[ImageMount("registry.example/tool@sha256:aaa", "/__tool")],
            secret_env=[SecretEnvVar("WORKLOAD_TOKEN", "secret-workload", "token")],
            secret_files=[
                SecretFile("/run/secrets/config", "secret-config", "config.json")
            ],
            _node_client_factory=FakeAsyncNodeClient,
        ) as sandbox:
            result = await sandbox.exec(["python", "-V"], check=True)
            self.assertEqual(sandbox.allocation_id, "alloc-1")

        self.assertEqual(result.stdout_text(), "async-ok\n")
        self.assertEqual(
            client.created_environment["registry_credential_id"], "sec-regcred"
        )
        self.assertEqual(client.created_run["image_mounts"][0].target, "/__tool")
        self.assertEqual(client.created_run["secret_env"][0].name, "WORKLOAD_TOKEN")
        self.assertEqual(
            client.created_run["secret_files"][0].path,
            "/run/secrets/config",
        )
        self.assertEqual(calls[0]["allocation_id"], "alloc-1")
        self.assertEqual(calls[1]["argv"], ["python", "-V"])
        self.assertEqual(client.cancelled[0][0], "run-1")
        self.assertEqual(client.deleted_environments[0][0], "env-1")

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
                self.run_wait_started = asyncio.Event()

            async def watch_run(self, run_id: str, **kwargs):
                del run_id, kwargs
                self.run_wait_started.set()
                await asyncio.sleep(3600)
                if False:
                    yield None

        client = SlowReplicaClient()
        sandbox = AsyncSandbox(
            client=client, image="docker.io/library/python:3.12-slim"
        )
        task = asyncio.create_task(sandbox.start())
        await asyncio.wait_for(client.run_wait_started.wait(), timeout=1.0)

        task.cancel()
        with self.assertRaises(asyncio.CancelledError):
            await task

        self.assertEqual(client.cancelled[0][0], "run-1")
        self.assertEqual(client.deleted_environments[0][0], "env-1")
        with self.assertRaises(SandboxNotStartedError):
            _ = sandbox.state


if __name__ == "__main__":
    unittest.main()
