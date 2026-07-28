#!/usr/bin/env python3
"""Record the public Python SDK flow and inspect its live Service with axern."""

from __future__ import annotations

import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys

REPO_DIR = Path(__file__).resolve().parents[3]
EXAMPLES_DIR = REPO_DIR / "sdk" / "python" / "examples"
sys.path.insert(0, str(EXAMPLES_DIR))
service_example_spec = importlib.util.spec_from_file_location(
    "service_gateway", EXAMPLES_DIR / "service_gateway.py"
)
if service_example_spec is None or service_example_spec.loader is None:
    raise RuntimeError("failed to load the Python Service example")
service_example = importlib.util.module_from_spec(service_example_spec)
service_example_spec.loader.exec_module(service_example)
run_service = service_example._run


def inspect_service(service_id: str) -> None:
    context = os.environ.get("AXERN_CONTEXT", "compose")
    axern = os.environ.get("AXERN_CLI_BINARY", str(REPO_DIR / "bin" / "axern"))
    print()
    print("$ axern service get <service-id> --output json", flush=True)
    result = subprocess.run(
        [
            axern,
            "--context",
            context,
            "service",
            "get",
            service_id,
            "--output",
            "json",
        ],
        cwd=REPO_DIR,
        check=False,
        capture_output=True,
        text=True,
    )
    if result.returncode != 0:
        sys.stderr.write(result.stderr)
        raise subprocess.CalledProcessError(result.returncode, result.args)

    service = json.loads(result.stdout)["service"]
    summary = {
        "status": service["status"],
        "ready_replicas": service["ready_replicas"],
        "replicas": service["replicas"],
    }
    print(json.dumps(summary, separators=(",", ":")), flush=True)


if __name__ == "__main__":
    if os.environ.get("AXERN_DOCS_RECORDING") == "1":
        print("\033[2J\033[Hrecording-ready", flush=True)
        input()
        print("\033[2J\033[H", end="", flush=True)
    print("$ uv run --package axern-sdk \\", flush=True)
    print("    python sdk/python/examples/service_gateway.py", flush=True)
    run_service(inspect_service)
