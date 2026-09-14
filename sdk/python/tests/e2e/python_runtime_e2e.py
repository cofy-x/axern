"""End-to-end Python SDK flow against real controld + axnoded."""

from __future__ import annotations

import argparse
import os
import sys
import time

from axern_sdk import AxernClient, EnvironmentCatalogClient, Sandbox
from axern.control.run.v1 import run_pb2


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--endpoint", required=True, help="controld gRPC address")
    parser.add_argument("--environment-id", default="python311", help="environment template id")
    parser.add_argument("--expected-image-ref", required=True, help="expected catalog image ref")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    tls = {
        "tls_ca_cert": os.getenv("AXERN_TLS_CA_CERT") or None,
        "tls_cert": os.getenv("AXERN_TLS_CERT") or None,
        "tls_key": os.getenv("AXERN_TLS_KEY") or None,
        "tls_server_name": os.getenv("AXERN_TLS_SERVER_NAME") or None,
        "proxy_mode": os.getenv("AXERN_PROXY_MODE", "env"),
    }
    catalog = EnvironmentCatalogClient(args.endpoint, **tls)
    client = AxernClient(args.endpoint, **tls)
    try:
        template = catalog.get_environment_template(args.environment_id)
        if template.id != args.environment_id:
            raise SystemExit(f"catalog returned unexpected runtime id: {template.id}")
        image_ref = template.resolved_spec.image_descriptor.annotations.get("org.opencontainers.image.ref.name", "")
        if image_ref != args.expected_image_ref:
            raise SystemExit(f"catalog returned image ref {image_ref!r}, want {args.expected_image_ref!r}")

        environment = client.create_environment(template_id=args.environment_id)
        if not environment.id:
            raise SystemExit("create_environment returned empty id")

        run = client.create_run(
            environment_id=environment.id,
            argv=["python", "-c", "print('python-sdk-ok')"],
        )
        if not run.id:
            raise SystemExit("create_run returned empty id")

        deadline = time.monotonic() + 30
        while run.status not in {
            run_pb2.RUN_STATUS_SUCCEEDED,
            run_pb2.RUN_STATUS_FAILED,
            run_pb2.RUN_STATUS_CANCELLED,
        }:
            if time.monotonic() >= deadline:
                raise SystemExit(f"run {run.id} did not finish within 30 seconds")
            time.sleep(0.2)
            run = client.runs.GetRun(run_pb2.GetRunRequest(run_id=run.id)).run
        if run.status != run_pb2.RUN_STATUS_SUCCEEDED or not run.exit_code_known or run.exit_code != 0:
            raise SystemExit(
                f"run {run.id} failed: status={run.status} "
                f"exit_code_known={run.exit_code_known} exit_code={run.exit_code}"
            )

        with Sandbox(client=client, environment_id=environment.id) as sandbox:
            result = sandbox.exec(
                ["python", "-c", "print('python-sandbox-sdk-ok')"],
                check=True,
                text=True,
            )
            if result.stdout.strip() != "python-sandbox-sdk-ok":
                raise SystemExit(f"sandbox exec returned unexpected output: {result.stdout!r}")

        print("verify_node_python_runtime_e2e_ok=true")
        return 0
    finally:
        client.close()
        catalog.close()


if __name__ == "__main__":
    sys.exit(main())
