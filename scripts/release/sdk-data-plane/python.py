from __future__ import annotations

import os
import tarfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from io import BytesIO
from pathlib import Path

import axern_sdk
from axern.control.tunnel.v1 import tunnel_pb2
from axern_sdk import (
    AxernClient,
    DeclaredOutput,
    DeclaredOutputFormat,
    ImageMount,
    Sandbox,
    SandboxError,
    TunnelConnector,
)


def main() -> None:
    config = required("AXERN_SDK_ACCEPTANCE_CONFIG")
    context = required("AXERN_SDK_ACCEPTANCE_CONTEXT")
    version = required("AXERN_SDK_ACCEPTANCE_VERSION")
    image_mount = required("AXERN_SDK_ACCEPTANCE_IMAGE_MOUNT")
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
            image_mounts=[ImageMount(image_mount, "/__runtime_probe")],
            declared_outputs=[
                DeclaredOutput(
                    path="/tmp/axern-candidate.txt",
                    format=DeclaredOutputFormat.FILE,
                    media_type="text/plain",
                ),
                DeclaredOutput(
                    path="/tmp/axern-trajectory",
                    format=DeclaredOutputFormat.TAR,
                    media_type="application/x-tar",
                ),
            ],
            labels={"axern.release.acceptance": "python"},
        ) as sandbox:
            assert_declared_outputs(
                client,
                sandbox.run_id,
                ["/tmp/axern-candidate.txt", "/tmp/axern-trajectory"],
            )
            assert_read_only_image_mount(client, sandbox)
            assert_public_tunnel(client, sandbox, marker)
            task_input = handshake / "python-task-input.txt"
            task_input.write_text(marker, encoding="utf-8")
            sandbox.upload_file(task_input, "/tmp/axern-task-input.txt")
            stream_events = list(
                sandbox.exec_stream(
                    [
                        "python",
                        "-c",
                        "import sys; print('stream-out'); print('stream-err', file=sys.stderr)",
                    ],
                    timeout_seconds=30,
                )
            )
            if (
                b"".join(
                    event.data for event in stream_events if event.stream == "stdout"
                ).strip()
                != b"stream-out"
            ):
                raise RuntimeError("installed wheel did not stream stdout")
            if (
                b"".join(
                    event.data for event in stream_events if event.stream == "stderr"
                ).strip()
                != b"stream-err"
            ):
                raise RuntimeError("installed wheel did not stream stderr")
            result = sandbox.exec(
                [
                    "python",
                    "-c",
                    "from pathlib import Path; "
                    "value=Path('/tmp/axern-task-input.txt').read_text(); "
                    "Path('/tmp/axern-candidate.txt').write_text(value); "
                    "Path('/tmp/axern-trajectory').mkdir(); "
                    "Path('/tmp/axern-trajectory/log.txt').write_text(value); "
                    "print(value)",
                ],
                check=True,
                text=True,
            )
            if result.stdout.strip() != marker:
                raise RuntimeError(
                    f"unexpected Python SDK exec output: {result.stdout!r}"
                )
            run_id = sandbox.run_id
        client.wait_run(run_id, timeout=60)
        outputs = wait_sealed_outputs(
            client,
            run_id,
            ["/tmp/axern-candidate.txt", "/tmp/axern-trajectory"],
        )
        destination = BytesIO()
        client.download_sealed_output(
            run_id,
            outputs["/tmp/axern-candidate.txt"].output_id,
            destination,
            timeout=60,
        )
        candidate = destination.getvalue()
        if candidate.decode().strip() != marker:
            raise RuntimeError("sealed declared output did not preserve the candidate")
        trajectory = BytesIO()
        client.download_sealed_output(
            run_id,
            outputs["/tmp/axern-trajectory"].output_id,
            trajectory,
            timeout=60,
        )
        assert_trajectory_archive(trajectory.getvalue(), marker)
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
            assert_declared_outputs(client, verifier.run_id, ["/tmp/verification.txt"])
            if client.get_run(verifier.run_id).config.image_mounts:
                raise RuntimeError(
                    "fresh verification Run inherited inference ImageMount"
                )
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
        verification = wait_sealed_outputs(
            client, verification_run_id, ["/tmp/verification.txt"]
        )["/tmp/verification.txt"]
        verified = BytesIO()
        client.download_sealed_output(
            verification_run_id, verification.output_id, verified, timeout=60
        )
        if verified.getvalue() != b"verified":
            raise RuntimeError("fresh verification Run returned an invalid result")
        handshake.joinpath("python.run-id").write_text(
            verification_run_id, encoding="utf-8"
        )
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


def assert_read_only_image_mount(client: AxernClient, sandbox: Sandbox) -> None:
    mounts = client.get_run(sandbox.run_id, timeout=10).config.image_mounts
    if len(mounts) != 1 or mounts[0].target != "/__runtime_probe":
        raise RuntimeError(f"Run did not preserve its ImageMount contract: {mounts!r}")
    sandbox.exec(
        [
            "/bin/sh",
            "-lc",
            "test -f /__runtime_probe/etc/os-release && "
            "! touch /__runtime_probe/axern-write-probe",
        ],
        check=True,
    )


def assert_public_tunnel(client: AxernClient, sandbox: Sandbox, marker: str) -> None:
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            body = marker.encode()
            self.send_response(200)
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, *_args) -> None:
            return

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    server_thread = threading.Thread(target=server.serve_forever, daemon=True)
    server_thread.start()
    session_id = ""
    connector: TunnelConnector | None = None
    try:
        response = client.create_tunnel_session(
            allocation_id=sandbox.allocation_id,
            ttl_seconds=45,
            wait_ready=True,
        )
        session_id = response.session.session_id
        connector = TunnelConnector(
            client=client,
            session=response.session,
            client_token=response.client_token,
            local_target=f"127.0.0.1:{server.server_port}",
        )
        connector.start()
        wait_tunnel_client(client, session_id)
        client.renew_tunnel_session(
            session_id,
            response.client_token,
            ttl_seconds=45,
        )
        endpoint = response.session.bound_addr or (
            f"127.0.0.1:{response.session.remote_port}"
        )
        result = sandbox.exec(
            [
                "python",
                "-c",
                "import urllib.request; "
                f"print(urllib.request.urlopen('http://{endpoint}', timeout=5).read().decode())",
            ],
            check=True,
            text=True,
        )
        if result.stdout.strip() != marker:
            raise RuntimeError("public TunnelConnector returned an invalid response")
        client.revoke_tunnel_session(session_id, reason="release acceptance complete")
        if not connector.wait_closed(25):
            raise RuntimeError("public TunnelConnector did not close after revoke")
        if sandbox.exec(["true"], check=True).exit_code != 0:
            raise RuntimeError("Tunnel revoke unexpectedly terminated the Allocation")
        session_id = ""
    finally:
        if connector is not None:
            connector.stop()
        if session_id:
            try:
                client.revoke_tunnel_session(session_id, reason="release cleanup")
            except Exception:
                pass
        server.shutdown()
        server.server_close()
        server_thread.join()


def wait_tunnel_client(client: AxernClient, session_id: str) -> None:
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        if any(
            event.event_type == tunnel_pb2.TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED
            for event in client.list_tunnel_events(session_id, limit=50)
        ):
            return
        time.sleep(0.1)
    raise RuntimeError("public TunnelConnector did not connect")


def assert_declared_outputs(
    client: AxernClient, run_id: str, expected_paths: list[str]
) -> None:
    run = client.get_run(run_id, timeout=10)
    paths = [output.path for output in run.config.declared_outputs]
    if paths != expected_paths:
        raise RuntimeError(
            f"Run did not preserve its declared-output contract: {paths!r}"
        )


def wait_sealed_outputs(client: AxernClient, run_id: str, expected_paths: list[str]):
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        try:
            outputs = client.get_sealed_output_manifest(run_id, timeout=10)
            by_path = {output.path: output for output in outputs}
            if set(by_path) != set(expected_paths):
                raise RuntimeError(f"unexpected sealed output manifest: {outputs!r}")
            if all(by_path[path].status == "available" for path in expected_paths):
                return by_path
        except SandboxError as error:
            if getattr(error, "code", "") not in {"NOT_FOUND", "UNAVAILABLE"}:
                raise
        time.sleep(0.2)
    raise TimeoutError("declared output was not sealed before the retention deadline")


def assert_trajectory_archive(payload: bytes, marker: str) -> None:
    with tarfile.open(fileobj=BytesIO(payload), mode="r:") as archive:
        members = [member for member in archive.getmembers() if member.isfile()]
        if len(members) != 1 or Path(members[0].name).name != "log.txt":
            raise RuntimeError(f"unexpected trajectory archive: {members!r}")
        extracted = archive.extractfile(members[0])
        if extracted is None or extracted.read().decode().strip() != marker:
            raise RuntimeError(
                "trajectory archive did not preserve its declared output"
            )


def required(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} is required")
    return value


if __name__ == "__main__":
    main()
