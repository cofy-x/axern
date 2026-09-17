from __future__ import annotations

import os
import time
from io import BytesIO
from pathlib import Path

import axern_sdk
from axern_sdk import AxernClient, DeclaredOutput, DeclaredOutputFormat, Sandbox, SandboxError


def main() -> None:
    config = required("AXERN_SDK_ACCEPTANCE_CONFIG")
    context = required("AXERN_SDK_ACCEPTANCE_CONTEXT")
    version = required("AXERN_SDK_ACCEPTANCE_VERSION")
    handshake = Path(required("AXERN_SDK_ACCEPTANCE_HANDSHAKE_DIR"))
    marker = "axern-python-sdk-release-ok"
    if axern_sdk.__version__ != version:
        raise RuntimeError(f"unexpected Python SDK version: {axern_sdk.__version__}")

    client = AxernClient.from_context(config, context)
    environment = None
    try:
        environment = client.create_environment(
            template_id="python311",
            labels={"axern.release.acceptance": "python"},
        )
        with Sandbox(
            client=client,
            environment_id=environment.id,
            request_cpu="100m",
            request_memory="512MiB",
            declared_outputs=[
                DeclaredOutput(
                    path="/tmp/axern-candidate.txt",
                    format=DeclaredOutputFormat.FILE,
                    media_type="text/plain",
                )
            ],
            labels={"axern.release.acceptance": "python"},
        ) as sandbox:
            assert_declared_output(client, sandbox.run_id, "/tmp/axern-candidate.txt")
            result = sandbox.exec(
                ["python", "-c", f"from pathlib import Path; Path('/tmp/axern-candidate.txt').write_text({marker!r}); print({marker!r})"],
                check=True,
                text=True,
            )
            if result.stdout.strip() != marker:
                raise RuntimeError(f"unexpected Python SDK exec output: {result.stdout!r}")
            run_id = sandbox.run_id
        client.wait_run(run_id, timeout=60)
        output = wait_sealed_output(client, run_id)
        destination = BytesIO()
        client.download_sealed_output(run_id, output.output_id, destination, timeout=60)
        candidate = destination.getvalue()
        if candidate.decode().strip() != marker:
            raise RuntimeError("sealed declared output did not preserve the candidate")
        with Sandbox(
            client=client,
            environment_id=environment.id,
            request_cpu="100m",
            request_memory="512MiB",
            declared_outputs=[
                DeclaredOutput(
                    path="/tmp/verification.txt",
                    format=DeclaredOutputFormat.FILE,
                    media_type="text/plain",
                )
            ],
            labels={"axern.release.acceptance": "python-verifier"},
        ) as verifier:
            assert_declared_output(client, verifier.run_id, "/tmp/verification.txt")
            verifier.write_file("/tmp/candidate.txt", candidate)
            verifier.exec(
                [
                    "python",
                    "-c",
                    "from pathlib import Path; candidate=Path('/tmp/candidate.txt').read_text().strip(); "
                    f"assert candidate == {marker!r}; Path('/tmp/verification.txt').write_text('verified')",
                ],
                check=True,
            )
            verification_run_id = verifier.run_id
        client.wait_run(verification_run_id, timeout=60)
        verification = wait_sealed_output(client, verification_run_id)
        verified = BytesIO()
        client.download_sealed_output(verification_run_id, verification.output_id, verified, timeout=60)
        if verified.getvalue() != b"verified":
            raise RuntimeError("fresh verification Run returned an invalid result")
        handshake.joinpath("python.run-id").write_text(verification_run_id, encoding="utf-8")
        wait_verified(handshake / "python.verified")
        print(
            f"sdk_data_plane=python inference_run_id={run_id} "
            f"verification_run_id={verification_run_id} sealed_output=true ok=true"
        )
    finally:
        if environment is not None:
            client.delete_environment(environment.id)
        client.close()


def wait_verified(path: Path) -> None:
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        if path.exists():
            return
        time.sleep(0.1)
    raise TimeoutError("CLI did not verify the Python SDK Run")


def assert_declared_output(client: AxernClient, run_id: str, expected_path: str) -> None:
    run = client.get_run(run_id, timeout=10)
    paths = [output.path for output in run.config.declared_outputs]
    if paths != [expected_path]:
        raise RuntimeError(
            f"Run did not preserve its declared-output contract: {paths!r}"
        )


def wait_sealed_output(client: AxernClient, run_id: str):
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        try:
            outputs = client.get_sealed_output_manifest(run_id, timeout=10)
            if len(outputs) != 1 or outputs[0].status != "available":
                raise RuntimeError(f"unexpected sealed output manifest: {outputs!r}")
            return outputs[0]
        except SandboxError as error:
            if getattr(error, "code", "") not in {"NOT_FOUND", "UNAVAILABLE"}:
                raise
        time.sleep(0.2)
    raise TimeoutError("declared output was not sealed before the retention deadline")


def required(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} is required")
    return value


if __name__ == "__main__":
    main()
