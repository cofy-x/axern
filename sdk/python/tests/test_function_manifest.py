from __future__ import annotations

import json
import tarfile
import tempfile
import unittest
from contextlib import contextmanager
from pathlib import Path

from typing import Any

from google.protobuf import duration_pb2

from axern.control.function.v1 import function_pb2, function_types_pb2
from axern_sdk import AxernClient, Function, FunctionInvocationResult, FunctionSpec, load_function_spec


class FunctionManifestTest(unittest.TestCase):
    def test_loads_golden_function_manifest(self) -> None:
        root = Path(__file__).resolve().parents[3]
        spec = load_function_spec(root / "examples/function-hello/function.yaml")

        self.assertIsInstance(spec, FunctionSpec)
        self.assertEqual(spec.name, "hello")
        self.assertEqual(spec.runtime, "python3.11")
        self.assertEqual(spec.handler, "handler.hello")
        self.assertEqual(spec.namespace, "default")
        self.assertEqual(spec.worker_source.template, "python311")
        self.assertEqual(spec.initializer, "handler.init")
        self.assertEqual(spec.source.root, "src")
        self.assertEqual(spec.timeout_seconds, 600)
        self.assertEqual(spec.resources.request_cpu, "600m")
        self.assertEqual(spec.resources.request_memory, "512MiB")
        self.assertEqual(spec.resources.limit_cpu, "1")
        self.assertEqual(spec.resources.limit_memory, "1GiB")
        self.assertEqual(spec.scaling.min_replicas, 0)
        self.assertEqual(spec.scaling.max_replicas, 10)
        self.assertEqual(spec.scaling.concurrency, 2)
        self.assertEqual(spec.scaling.idle_seconds, 300)
        self.assertEqual(spec.env, {"GREETING": "hello"})
        self.assertEqual(spec.volumes, ())
        self.assertEqual(spec.root_dir, (root / "examples/function-hello").resolve())
        self.assertEqual(spec.manifest_path.name, "function.yaml")

    def test_function_from_file_loads_spec_without_deploying(self) -> None:
        root = Path(__file__).resolve().parents[3]
        client = AxernClient.__new__(AxernClient)

        function = Function.from_file(client, root / "examples/function-hello/function.yaml")

        self.assertIs(function.client, client)
        self.assertEqual(function.name, "hello")
        self.assertEqual(function.spec.handler, "handler.hello")

    def test_invoke_returns_decoded_result(self) -> None:
        with self._function_dir({}) as path:
            client = _FakeClient()
            function = Function.from_file(client, path)

            result = function.invoke({"name": "axern"}, namespace="team-a")

            self.assertIsInstance(result, FunctionInvocationResult)
            self.assertEqual(result.invocation_id, "inv-1")
            self.assertEqual(result.function_name, "hello")
            self.assertEqual(result.status, "succeeded")
            self.assertEqual(result.value, {"message": "hello axern"})
            self.assertIsNone(result.error)
            invoke_req = client.functions.invoke_request
            self.assertIsNotNone(invoke_req)
            assert invoke_req is not None
            self.assertEqual(invoke_req.namespace, "team-a")
            self.assertEqual(invoke_req.name, "hello")
            self.assertEqual(json.loads(invoke_req.payload.data.decode("utf-8")), {"name": "axern"})

    def test_invoke_returns_error_on_failure(self) -> None:
        with self._function_dir({}) as path:
            client = _FakeClient(invoke_error=True)
            function = Function.from_file(client, path)

            result = function.invoke({"name": "axern"})

            self.assertEqual(result.status, "failed")
            self.assertIsNone(result.value)
            self.assertIsNotNone(result.error)
            assert result.error is not None
            self.assertEqual(result.error.code, "HANDLER_ERROR")
            self.assertEqual(result.error.message, "something went wrong")

    def test_package_writes_deterministic_bundle(self) -> None:
        with self._function_dir({}) as path, tempfile.TemporaryDirectory(prefix="axern-function-out-") as output:
            function = Function.from_file(_FakeClient(), path)

            first = function.package(output)
            second = function.package(output)

            self.assertEqual(first.digest, second.digest)
            self.assertEqual(first.size_bytes, second.size_bytes)
            self.assertEqual(first.media_type, "application/vnd.axern.function.tar")
            self.assertIsNotNone(first.path)
            assert first.path is not None
            self.assertTrue(first.path.is_file())
            with tarfile.open(first.path, "r") as bundle:
                self.assertEqual(bundle.getnames(), ["src/handler.py"])

    def test_rejects_source_root_equal_to_spec_directory(self) -> None:
        with self._function_dir({"source": {"root": "."}}) as path:
            with self.assertRaisesRegex(ValueError, "below the spec directory"):
                Function.from_file(_FakeClient(), path)

    def test_package_rejects_output_dir_inside_source_root(self) -> None:
        with self._function_dir({}) as path:
            function = Function.from_file(_FakeClient(), path)

            with self.assertRaisesRegex(ValueError, "outside manifest.source.root"):
                function.package(Path(path).parent / "src" / "dist")

    def test_deploy_packages_and_calls_function_control(self) -> None:
        with self._function_dir(
            {
                "timeout_seconds": 30,
                "env": {"GREETING": "hello"},
                "extension_capabilities": {"example.com/accelerator": "v1"},
                "resources": {"request_cpu": "500m", "limit_memory": "1GiB"},
                "scaling": {"min_replicas": 1, "max_replicas": 3, "concurrency": 2, "idle_seconds": 90},
                "volumes": [{"name": "data", "target": "/data", "readonly": True, "options": ["rbind"]}],
                "secret_env": [{"name": "TOKEN", "secret_id": "secret-a", "key": "token"}],
                "secret_files": [{"path": "/run/secrets/config", "secret_id": "secret-a", "key": "config", "mode": "0440"}],
                "image_mounts": [{"image": "example.test/tools:latest", "target": "/opt/tools"}],
            }
        ) as path:
            client = _FakeClient()
            function = Function.from_file(client, path)

            response = function.deploy(namespace="team-a", labels={"team": "runtime"}, timeout=7.0)

            self.assertEqual(response.function.id, "fn-1")
            request = client.functions.request
            self.assertIsNotNone(request)
            assert request is not None
            self.assertEqual(client.functions.timeout, 7.0)
            self.assertIsNotNone(client.functions.upload_open)
            assert client.functions.upload_open is not None
            self.assertEqual(client.functions.upload_open.namespace, "team-a")
            self.assertEqual(client.functions.upload_open.name, "hello")
            self.assertTrue(client.functions.upload_open.digest.startswith("sha256:"))
            self.assertEqual(client.functions.upload_open.media_type, "application/vnd.axern.function.tar")
            self.assertGreater(client.functions.upload_open.size_bytes, 0)
            self.assertEqual(client.functions.upload_size, client.functions.upload_open.size_bytes)
            self.assertEqual(request.namespace, "team-a")
            self.assertEqual(request.name, "hello")
            self.assertEqual(request.labels, {"team": "runtime"})
            self.assertFalse(request.wait_ready)
            self.assertEqual(request.spec.runtime, "python3.11")
            self.assertEqual(request.spec.handler, "handler.hello")
            self.assertEqual(request.spec.worker_source.environment.template_id, "python311")
            self.assertEqual(request.spec.worker_source.environment.namespace, "team-a")
            self.assertEqual(request.spec.timeout.seconds, 30)
            self.assertEqual(request.spec.config.env, {"GREETING": "hello"})
            extension = request.spec.config.extension_capability_requirements[0].capability
            self.assertEqual(extension.name, "example.com/accelerator")
            self.assertEqual(extension.value, "v1")
            self.assertEqual(request.spec.config.resources.requests.cpu_milli, 500)
            self.assertEqual(request.spec.config.resources.limits.memory_bytes, 1024 * 1024 * 1024)
            self.assertEqual(request.spec.config.volume_mounts[0].name, "data")
            self.assertEqual(request.spec.config.volume_mounts[0].target, "/data")
            self.assertTrue(request.spec.config.volume_mounts[0].readonly)
            self.assertEqual(request.spec.config.volume_mounts[0].options, ["rbind"])
            self.assertEqual(request.spec.config.secret_env[0].secret_id, "secret-a")
            self.assertEqual(request.spec.config.secret_files[0].mode, 0o440)
            self.assertTrue(request.spec.config.image_mounts[0].readonly)
            self.assertEqual(request.spec.scaling.min_replicas, 1)
            self.assertEqual(request.spec.scaling.max_replicas, 3)
            self.assertEqual(request.spec.scaling.concurrency, 2)
            self.assertEqual(request.spec.scaling.idle_timeout.seconds, 90)
            self.assertEqual(request.source.bundle.digest, client.functions.upload_open.digest)
            self.assertEqual(request.source.bundle.size_bytes, client.functions.upload_open.size_bytes)
            self.assertEqual(request.source.bundle.media_type, "application/vnd.axern.function.tar")
            self.assertEqual(request.source.bundle.storage_uri, "axern://function-bundles/uploaded.tar")

    def test_rejects_unknown_fields(self) -> None:
        with self._function_dir({"unexpected": True}) as path:
            with self.assertRaisesRegex(ValueError, "unsupported field"):
                load_function_spec(path)

    def test_rejects_bad_handler_reference(self) -> None:
        with self._function_dir({"handler": "not-a-python-ref"}) as path:
            with self.assertRaisesRegex(ValueError, "module.callable"):
                load_function_spec(path)

    def test_rejects_missing_source_root(self) -> None:
        with self._function_dir({"source": {"root": "missing"}}) as path:
            with self.assertRaisesRegex(ValueError, "spec.function.source"):
                load_function_spec(path)

    def test_rejects_duplicate_volume_targets(self) -> None:
        with self._function_dir(
            {
                "volumes": [
                    {"name": "data", "target": "/data"},
                    {"name": "cache", "target": "/data"},
                ]
            }
        ) as path:
            with self.assertRaisesRegex(ValueError, "invalid or duplicate"):
                load_function_spec(path)

    def test_rejects_invalid_resource_quantity(self) -> None:
        with self._function_dir({"resources": {"request_memory": "a lot"}}) as path:
            with self.assertRaises(ValueError):
                load_function_spec(path)

    @contextmanager
    def _function_dir(self, overrides: dict[str, Any]):
        with tempfile.TemporaryDirectory(prefix="axern-function-") as temp:
            root = Path(temp)
            root.joinpath("src").mkdir()
            root.joinpath("src/handler.py").write_text("def hello(event, context):\n    return event\n", encoding="utf-8")
            function = {"runtime": "python3.11", "handler": "handler.hello", "source": "src"}
            spec: dict[str, Any] = {
                "api_version": "axern/v1",
                "kind": "Function",
                "metadata": {"name": "hello"},
                "spec": {"source": {"template": "python311"}, "function": function},
            }
            for key, value in overrides.items():
                if key == "source":
                    function["source"] = value["root"]
                elif key in {"handler", "initializer", "timeout_seconds"}:
                    function[key] = value
                elif key == "scaling":
                    function["scaling"] = {
                        "min_replicas": value.get("min_replicas", 0),
                        "max_replicas": value.get("max_replicas", 1),
                        "concurrency": value.get("concurrency", 1),
                        "idle_timeout": f"{value.get('idle_seconds', 300)}s",
                    }
                elif key == "resources":
                    spec["spec"]["resources"] = {
                        "requests": {"cpu": value.get("request_cpu", ""), "memory": value.get("request_memory", "")},
                        "limits": {"cpu": value.get("limit_cpu", ""), "memory": value.get("limit_memory", "")},
                    }
                else:
                    spec["spec"][key] = value
            spec_path = root / "function.json"
            spec_path.write_text(json.dumps(spec), encoding="utf-8")
            yield spec_path


class _FakeFunctions:
    def __init__(self, *, invoke_error: bool = False) -> None:
        self.request: function_pb2.DeployFunctionRequest | None = None
        self.timeout: float | None = None
        self.upload_open: function_pb2.UploadFunctionBundleOpen | None = None
        self.upload_size = 0
        self.invoke_request: function_pb2.InvokeFunctionRequest | None = None
        self._invoke_error = invoke_error

    def UploadFunctionBundle(
        self,
        requests,
        *,
        timeout: float | None = None,
    ) -> function_pb2.UploadFunctionBundleResponse:
        self.timeout = timeout
        for index, request in enumerate(requests):
            if index == 0:
                self.upload_open = request.open
                continue
            self.upload_size += len(request.chunk)
        assert self.upload_open is not None
        return function_pb2.UploadFunctionBundleResponse(
            bundle=function_types_pb2.FunctionBundleSource(
                digest=self.upload_open.digest,
                media_type=self.upload_open.media_type,
                size_bytes=self.upload_open.size_bytes,
                storage_uri="axern://function-bundles/uploaded.tar",
            )
        )

    def DeployFunction(
        self,
        request: function_pb2.DeployFunctionRequest,
        *,
        timeout: float | None = None,
    ) -> function_pb2.DeployFunctionResponse:
        self.request = request
        self.timeout = timeout
        return function_pb2.DeployFunctionResponse(
            function=function_types_pb2.Function(id="fn-1", name=request.name, namespace=request.namespace)
        )

    def InvokeFunction(
        self,
        request: function_pb2.InvokeFunctionRequest,
        *,
        timeout: float | None = None,
    ) -> function_pb2.InvokeFunctionResponse:
        self.invoke_request = request
        self.timeout = timeout
        if self._invoke_error:
            return function_pb2.InvokeFunctionResponse(
                invocation=function_types_pb2.FunctionInvocation(
                    id="inv-err",
                    function_id="fn-1",
                    function_name=request.name,
                    namespace=request.namespace,
                    status=function_types_pb2.FUNCTION_INVOCATION_STATUS_FAILED,
                    error=function_types_pb2.FunctionError(
                        code="HANDLER_ERROR",
                        message="something went wrong",
                    ),
                )
            )
        payload_data = request.payload.data if request.payload else b"{}"
        event = json.loads(payload_data.decode("utf-8"))
        result_data = json.dumps({"message": f"hello {event.get('name', 'world')}"}).encode("utf-8")
        return function_pb2.InvokeFunctionResponse(
            invocation=function_types_pb2.FunctionInvocation(
                id="inv-1",
                function_id="fn-1",
                function_name=request.name,
                namespace=request.namespace,
                status=function_types_pb2.FUNCTION_INVOCATION_STATUS_SUCCEEDED,
                result=function_types_pb2.FunctionResult(
                    content_type="application/json",
                    data=result_data,
                ),
                duration=duration_pb2.Duration(seconds=1),
            )
        )


class _FakeClient:
    def __init__(self, *, invoke_error: bool = False) -> None:
        self.functions = _FakeFunctions(invoke_error=invoke_error)


if __name__ == "__main__":
    unittest.main()
