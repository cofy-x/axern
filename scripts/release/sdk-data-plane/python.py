from __future__ import annotations

import os
import time
from pathlib import Path

import axern_sdk
from axern_sdk import AxernClient, Sandbox


def main() -> None:
    config = required("AXERN_SDK_ACCEPTANCE_CONFIG")
    context = required("AXERN_SDK_ACCEPTANCE_CONTEXT")
    version = required("AXERN_SDK_ACCEPTANCE_VERSION")
    handshake = Path(required("AXERN_SDK_ACCEPTANCE_HANDSHAKE_DIR"))
    marker = "axern-python-sdk-release-ok"
    if axern_sdk.__version__ != version:
        raise RuntimeError(f"unexpected Python SDK version: {axern_sdk.__version__}")

    client = AxernClient.from_context(config, context)
    try:
        with Sandbox(
            client=client,
            template_id="python311",
            runtime_class="runsc",
            request_cpu="100m",
            request_memory="512MiB",
            labels={"axern.release.acceptance": "python"},
        ) as sandbox:
            result = sandbox.exec(["python", "-c", f"print({marker!r})"], check=True, text=True)
            if result.stdout.strip() != marker:
                raise RuntimeError(f"unexpected Python SDK exec output: {result.stdout!r}")
            handshake.joinpath("python.service-id").write_text(sandbox.service_id, encoding="utf-8")
            wait_verified(handshake / "python.verified")
            print(f"sdk_data_plane=python service_id={sandbox.service_id} ok=true")
    finally:
        client.close()


def wait_verified(path: Path) -> None:
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        if path.exists():
            return
        time.sleep(0.1)
    raise TimeoutError("CLI did not verify the Python SDK service")


def required(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} is required")
    return value


if __name__ == "__main__":
    main()
