"""Compose smoke for SDK Function deploy plus gateway-backed InvokeFunction."""

from __future__ import annotations

import argparse
import json
import sys
import time
from pathlib import Path
from tempfile import TemporaryDirectory

from google.protobuf import duration_pb2

from axern.control.function.v1 import function_pb2, function_types_pb2
from axern_sdk import AxernClient, Function


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--endpoint", required=True)
    parser.add_argument("--tls-ca-cert", required=True)
    parser.add_argument("--tls-cert", required=True)
    parser.add_argument("--tls-key", required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    namespace = f"compose-function-smoke-{int(time.time())}"
    name = "hello"
    client = AxernClient(
        args.endpoint,
        tls_ca_cert=args.tls_ca_cert,
        tls_cert=args.tls_cert,
        tls_key=args.tls_key,
    )
    function_id = None
    try:
        with TemporaryDirectory(prefix="axern-function-smoke-") as tmp:
            root = _write_function(Path(tmp), name)
            function = Function.from_file(client, root / "function.yaml")
            deployed = function.deploy(namespace=namespace, labels={"smoke": "function"}, timeout=120.0)
            function_id = deployed.function.id
            _wait_ready(client, deployed.function.id)
            invoked = client.functions.InvokeFunction(
                function_pb2.InvokeFunctionRequest(
                    namespace=namespace,
                    name=name,
                    mode=function_types_pb2.FUNCTION_INVOCATION_MODE_SYNC,
                    payload=function_types_pb2.FunctionPayload(
                        content_type="application/json",
                        data=json.dumps({"name": "Compose"}).encode("utf-8"),
                    ),
                    timeout=duration_pb2.Duration(seconds=30),
                    request_id=f"function-smoke-{int(time.time())}",
                    labels={"smoke": "function"},
                ),
                timeout=120.0,
            )
            _assert_invocation(invoked.invocation)
    finally:
        if function_id:
            try:
                client.functions.DeleteFunction(
                    function_pb2.DeleteFunctionRequest(function_id=function_id),
                    timeout=60.0,
                )
            except Exception as exc:  # noqa: BLE001
                print(f"function_cleanup_failed={exc}", file=sys.stderr)
        client.close()
    print("function_invocation_succeeded=true")
    return 0


def _write_function(root: Path, name: str) -> Path:
    source = root / "src"
    source.mkdir(parents=True)
    (root / "function.yaml").write_text(
        f"""api_version: axern/v1
kind: Function
metadata:
  name: {name}
  namespace: default
spec:
  source:
    template: python311
  env:
    GREETING: hello
  function:
    runtime: python3.11
    handler: handler.hello
    initializer: handler.init
    source: src
    timeout_seconds: 30
    scaling:
      min_replicas: 1
      max_replicas: 1
      concurrency: 1
      idle_timeout: 1m
""",
        encoding="utf-8",
    )
    (source / "handler.py").write_text(
        """
def init(context):
    return {"initialized": True, "function": context.function_name}


def hello(event, context):
    return {
        "message": f"{context.env.get('GREETING', 'hello')} {event.get('name', 'world')}",
        "request_id": context.request_id,
        "state": context.state,
    }
""".lstrip(),
        encoding="utf-8",
    )
    return root


def _wait_ready(client: AxernClient, function_id: str) -> None:
    deadline = time.monotonic() + 180
    last = None
    while time.monotonic() < deadline:
        last = client.functions.GetFunction(
            function_pb2.GetFunctionRequest(function_id=function_id),
            timeout=30.0,
        )
        if (
            last.deployment.status == function_types_pb2.FUNCTION_DEPLOYMENT_STATUS_READY
            and last.deployment.ready_replicas > 0
        ):
            return
        time.sleep(2)
    status = function_types_pb2.FunctionDeploymentStatus.Name(last.deployment.status) if last else "unknown"
    raise RuntimeError(f"function worker did not become ready: {status}")


def _assert_invocation(invocation: function_types_pb2.FunctionInvocation) -> None:
    if invocation.status != function_types_pb2.FUNCTION_INVOCATION_STATUS_SUCCEEDED:
        raise RuntimeError(f"function invocation failed: {invocation.error}")
    payload = json.loads(invocation.result.data.decode("utf-8"))
    if payload.get("message") != "hello Compose":
        raise RuntimeError(f"unexpected function result: {payload!r}")
    if not payload.get("state", {}).get("initialized"):
        raise RuntimeError(f"function initializer state missing: {payload!r}")


if __name__ == "__main__":
    raise SystemExit(main())
