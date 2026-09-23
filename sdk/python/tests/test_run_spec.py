from __future__ import annotations

import inspect
import unittest
from dataclasses import fields
from typing import Any, cast

from axern.control.common.v1 import common_pb2
from axern.control.run.v1 import run_pb2
from axern_sdk import (
    AsyncAxernClient,
    AsyncSandbox,
    AxernClient,
    ImageMount,
    NetworkPolicy,
    Sandbox,
    SecretEnvVar,
    SecretFile,
)


class _RunStub:
    def __init__(self) -> None:
        self.request: run_pb2.CreateRunRequest | None = None

    def CreateRun(self, request, timeout=None):
        del timeout
        self.request = request
        return run_pb2.CreateRunResponse(run=run_pb2.Run(id="run-1"))


class _AsyncRunStub:
    def __init__(self) -> None:
        self.request: run_pb2.CreateRunRequest | None = None

    async def CreateRun(self, request, timeout=None):
        del timeout
        self.request = request
        return run_pb2.CreateRunResponse(run=run_pb2.Run(id="run-1"))


class RunSpecTest(unittest.TestCase):
    def test_deny_all_and_omitted_policy_have_distinct_wire_contracts(self) -> None:
        client = AxernClient.__new__(AxernClient)
        stub = _RunStub()
        cast(Any, client).runs = stub

        client.create_run(environment_id="env-1", network_policy=NetworkPolicy.deny_all())
        assert stub.request is not None
        policy = stub.request.config.network.egress_policy
        self.assertEqual(policy.WhichOneof("policy"), "strict")
        self.assertEqual(len(policy.strict.allowed_domains), 0)
        self.assertEqual(len(policy.strict.allowed_cidrs), 0)

        client.create_run(environment_id="env-1")
        assert stub.request is not None
        self.assertFalse(stub.request.config.HasField("network"))

    def test_sync_create_run_builds_projection_contract(self) -> None:
        client = AxernClient.__new__(AxernClient)
        stub = _RunStub()
        cast(Any, client).runs = stub

        client.create_run(
            environment_id="env-1",
            rootfs_snapshot=True,
            image_mounts=(
                ImageMount("registry.example/tool@sha256:aaa", "/__tool"),
                ImageMount("registry.example/data@sha256:bbb", "/__data"),
            ),
            secret_env=(
                SecretEnvVar("WORKLOAD_TOKEN", "secret-workload", "token"),
                SecretEnvVar(
                    "OPTIONAL_TOKEN",
                    "secret-optional",
                    "token",
                    optional=True,
                ),
            ),
            secret_files=(
                SecretFile(
                    "/run/secrets/config",
                    "secret-config",
                    "config.json",
                    mode=0o440,
                ),
            ),
        )

        assert stub.request is not None
        config = stub.request.config
        self.assertEqual(
            [(item.image, item.target) for item in config.image_mounts],
            [
                ("registry.example/tool@sha256:aaa", "/__tool"),
                ("registry.example/data@sha256:bbb", "/__data"),
            ],
        )
        self.assertTrue(config.HasField("rootfs_snapshot"))
        self.assertEqual(
            [
                (item.name, item.secret_id, item.key, item.optional)
                for item in config.secret_env
            ],
            [
                ("WORKLOAD_TOKEN", "secret-workload", "token", False),
                ("OPTIONAL_TOKEN", "secret-optional", "token", True),
            ],
        )
        self.assertEqual(
            [
                (item.path, item.secret_id, item.key, item.mode, item.optional)
                for item in config.secret_files
            ],
            [
                (
                    "/run/secrets/config",
                    "secret-config",
                    "config.json",
                    0o440,
                    False,
                )
            ],
        )

    def test_empty_projection_collections_stay_empty(self) -> None:
        client = AxernClient.__new__(AxernClient)
        stub = _RunStub()
        cast(Any, client).runs = stub

        client.create_run(
            environment_id="env-1",
            image_mounts=(),
            secret_env=(),
            secret_files=(),
        )

        assert stub.request is not None
        self.assertEqual(list(stub.request.config.image_mounts), [])
        self.assertEqual(list(stub.request.config.secret_env), [])
        self.assertEqual(list(stub.request.config.secret_files), [])

    def test_public_facades_expose_the_same_projection_parameters(self) -> None:
        names = ("image_mounts", "secret_env", "secret_files")
        for target in (
            AxernClient.create_run,
            AsyncAxernClient.create_run,
            Sandbox,
            AsyncSandbox,
        ):
            with self.subTest(target=target):
                parameters = inspect.signature(target).parameters
                for name in names:
                    self.assertIn(name, parameters)

        self.assertIn(
            "rootfs_snapshot", inspect.signature(AxernClient.create_run).parameters
        )
        self.assertIn(
            "rootfs_snapshot", inspect.signature(AsyncAxernClient.create_run).parameters
        )
        self.assertNotIn("rootfs_snapshot", inspect.signature(Sandbox).parameters)
        self.assertNotIn("rootfs_snapshot", inspect.signature(AsyncSandbox).parameters)

    def test_public_models_cannot_carry_plaintext_or_writable_mount_intent(
        self,
    ) -> None:
        self.assertEqual(
            [field.name for field in fields(ImageMount)], ["image", "target"]
        )
        self.assertEqual(
            [field.name for field in fields(SecretEnvVar)],
            ["name", "secret_id", "key", "optional"],
        )
        self.assertEqual(
            [field.name for field in fields(SecretFile)],
            ["path", "secret_id", "key", "mode", "optional"],
        )
        self.assertEqual(
            [field.name for field in common_pb2.ImageMount.DESCRIPTOR.fields],
            ["image", "target"],
        )


class AsyncRunSpecTest(unittest.IsolatedAsyncioTestCase):
    async def test_async_deny_all_preserves_empty_strict_policy(self) -> None:
        client = AsyncAxernClient.__new__(AsyncAxernClient)
        stub = _AsyncRunStub()
        raw_client = cast(Any, client)
        raw_client._channel = object()
        raw_client._loop = None
        raw_client._runs = stub
        raw_client._environments = object()
        raw_client._tunnels = object()

        await client.create_run(environment_id="env-1", network_policy=NetworkPolicy.deny_all())
        assert stub.request is not None
        self.assertEqual(stub.request.config.network.egress_policy.WhichOneof("policy"), "strict")

    async def test_async_create_run_builds_projection_contract(self) -> None:
        client = AsyncAxernClient.__new__(AsyncAxernClient)
        stub = _AsyncRunStub()
        raw_client = cast(Any, client)
        raw_client._channel = object()
        raw_client._loop = None
        raw_client._runs = stub
        raw_client._environments = object()
        raw_client._tunnels = object()

        await client.create_run(
            environment_id="env-1",
            image_mounts=[ImageMount("registry.example/tool@sha256:aaa", "/__tool")],
            secret_env=[SecretEnvVar("WORKLOAD_TOKEN", "secret-workload", "token")],
            secret_files=[
                SecretFile(
                    "/run/secrets/config",
                    "secret-config",
                    "config.json",
                    optional=True,
                )
            ],
        )

        assert stub.request is not None
        config = stub.request.config
        self.assertEqual(config.image_mounts[0].target, "/__tool")
        self.assertEqual(config.secret_env[0].secret_id, "secret-workload")
        self.assertTrue(config.secret_files[0].optional)


if __name__ == "__main__":
    unittest.main()
