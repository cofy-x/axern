from __future__ import annotations

import json
import inspect
import os
import tempfile
import unittest
from pathlib import Path

import grpc

from axern_sdk._internal.errors import sandbox_rpc_error
from axern_sdk._internal.resources import cpu_milli, memory_bytes
from axern_sdk.client import AxernClient
from axern_sdk.context import load_context
from axern_sdk.errors import (
    SandboxCancelledError,
    SandboxConnectionError,
    SandboxNotFoundError,
    SandboxPermissionError,
    SandboxTimeoutError,
    SandboxValidationError,
)
from axern_sdk.sandbox import Sandbox
from axern_sdk.sandbox.types import _validate_source


CONTRACT_ROOT = Path(__file__).resolve().parents[2] / "contracts" / "v1"


class FakeRpcError(grpc.RpcError):
    def __init__(self, code: grpc.StatusCode) -> None:
        super().__init__()
        self._code = code

    def code(self) -> grpc.StatusCode:
        return self._code

    def details(self) -> str:
        return self._code.name


class ContractV1Test(unittest.TestCase):
    def test_resource_quantities(self) -> None:
        contract = load("resources.json")
        for item in contract["cpu"]:
            self.assertEqual(cpu_milli("cpu", item["input"]), item["value"])
        for item in contract["memory"]:
            self.assertEqual(memory_bytes("memory", item["input"]), item["value"])
        for value in contract["invalid_cpu"]:
            with self.assertRaises(ValueError):
                cpu_milli("cpu", value)
        for value in contract["invalid_memory"]:
            with self.assertRaises(ValueError):
                memory_bytes("memory", value)

    def test_rpc_errors(self) -> None:
        contract = load("errors.json")
        classes = {
            "not_found": SandboxNotFoundError,
            "permission_denied": SandboxPermissionError,
            "timeout": SandboxTimeoutError,
            "cancelled": SandboxCancelledError,
            "unavailable": SandboxConnectionError,
        }
        for item in contract["rpc"]:
            code = grpc.StatusCode[item["code"]]
            error = sandbox_rpc_error(FakeRpcError(code), operation="contract", allocation_id="alloc-contract")
            self.assertIsInstance(error, classes[item["class"]])
            self.assertEqual(error.retryable, item["retryable"])
            self.assertEqual(error.operation, "contract")
            self.assertEqual(error.allocation_id, "alloc-contract")

    def test_sandbox_sources(self) -> None:
        contract = load("sandbox_sources.json")
        for item in contract["valid"]:
            _validate_source(
                template_id=item.get("template", ""),
                image=item.get("image", ""),
                environment_id=item.get("environment", ""),
            )
        for item in contract["invalid"]:
            with self.assertRaises(SandboxValidationError):
                _validate_source(
                    template_id=item.get("template", ""),
                    image=item.get("image", ""),
                    environment_id=item.get("environment", ""),
                )

    def test_contexts(self) -> None:
        contract = load("contexts.json")
        for item in contract["valid"]:
            context = load_context(write_context(self, item["name"], item["context"]))
            self.assertEqual(context.name, item["name"])
            self.assertEqual(context.endpoint, item["context"]["endpoint"])
            self.assertEqual(context.proxy_mode, item["context"]["proxy_mode"])
        for item in contract["invalid"]:
            with self.assertRaises(ValueError):
                load_context(write_context(self, item["name"], item["context"]))

    def test_common_core_surface(self) -> None:
        contract = load("common_core.json")
        client_methods = {
            "environment_create": "create_environment",
            "environment_delete": "delete_environment",
            "service_create": "create_service",
            "service_delete": "delete_service",
            "service_replicas": "list_service_replicas",
        }
        sandbox_methods = {
            "lifecycle_start": "start",
            "lifecycle_close": "close",
            "exec": "exec",
            "process": "process",
            "file_stat": "stat",
            "file_read": "read_file",
            "file_write": "write_file",
            "archive_upload": "upload_dir",
            "archive_download": "download_dir",
        }
        assert_methods(self, contract["client"], AxernClient, client_methods)
        assert_methods(self, [item for item in contract["sandbox"] if item != "tunnel"], Sandbox, sandbox_methods)
        parameters = inspect.signature(Sandbox).parameters
        self.assertIn("upstream", parameters)
        self.assertIn("remote_port", parameters)


def load(name: str) -> dict[str, object]:
    return json.loads((CONTRACT_ROOT / name).read_text(encoding="utf-8"))


def write_context(test: unittest.TestCase, name: str, context: dict[str, object]) -> str:
    handle = tempfile.NamedTemporaryFile(mode="w", encoding="utf-8", suffix=".json", delete=False)
    with handle:
        json.dump({"current_context": name, "contexts": {name: context}}, handle)
    test.addCleanup(os.unlink, handle.name)
    return handle.name


def assert_methods(
    test: unittest.TestCase,
    operations: list[str],
    target: type[object],
    mapping: dict[str, str],
) -> None:
    for operation in operations:
        test.assertIn(operation, mapping, f"shared operation {operation!r} has no Python SDK mapping")
        test.assertTrue(hasattr(target, mapping[operation]), f"Python SDK method {mapping[operation]!r} is missing")
