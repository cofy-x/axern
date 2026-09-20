from __future__ import annotations

import os
import tarfile
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from io import BytesIO
from pathlib import Path

import axern_sdk
from axern.control.common.v1 import common_pb2
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
from contract import normalized_declared_output_contract


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
            image_ref="python:3.12-slim",
            labels={"axern.release.acceptance": "python"},
        )
        declared_outputs = [
            DeclaredOutput(
                path="/tmp/axern-output.txt",
                format=DeclaredOutputFormat.FILE,
                media_type="text/plain",
            ),
            DeclaredOutput(
                path="/tmp/axern-output-dir",
                format=DeclaredOutputFormat.TAR,
                media_type="application/x-tar",
            ),
        ]
        with Sandbox(
            client=client,
            environment_id=environment.id,
            request_cpu="100m",
            request_memory="512MiB",
            image_mounts=[ImageMount(image_mount, "/__runtime_probe")],
            declared_outputs=declared_outputs,
            labels={"axern.release.acceptance": "python"},
        ) as sandbox:
            assert_declared_outputs(client, sandbox.run_id, declared_outputs)
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
                    "Path('/tmp/axern-output.txt').write_text(value); "
                    "Path('/tmp/axern-output-dir').mkdir(); "
                    "Path('/tmp/axern-output-dir/log.txt').write_text(value); "
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
            ["/tmp/axern-output.txt", "/tmp/axern-output-dir"],
        )
        destination = BytesIO()
        client.download_sealed_output(
            run_id,
            outputs["/tmp/axern-output.txt"].output_id,
            destination,
            timeout=60,
        )
        output = destination.getvalue()
        if output.decode().strip() != marker:
            raise RuntimeError("sealed declared output did not preserve the file")
        output_archive = BytesIO()
        client.download_sealed_output(
            run_id,
            outputs["/tmp/axern-output-dir"].output_id,
            output_archive,
            timeout=60,
        )
        assert_output_archive(output_archive.getvalue(), marker)
        second_declared_outputs = [
            DeclaredOutput(
                path="/tmp/second-output.txt",
                format=DeclaredOutputFormat.FILE,
                media_type="text/plain",
            )
        ]
        with Sandbox(
            client=client,
            environment_id=environment.id,
            request_cpu="100m",
            request_memory="512MiB",
            declared_outputs=second_declared_outputs,
            labels={"axern.release.acceptance": "python-second-run"},
        ) as second:
            assert_declared_outputs(client, second.run_id, second_declared_outputs)
            if client.get_run(second.run_id).config.image_mounts:
                raise RuntimeError("second independent Run inherited an ImageMount")
            second.write_file("/tmp/input.txt", output)
            second.exec(
                [
                    "python",
                    "-c",
                    "from pathlib import Path; value=Path('/tmp/input.txt').read_text().strip(); "
                    f"assert value == {marker!r}; Path('/tmp/second-output.txt').write_text('complete')",
                ],
                check=True,
            )
            second_run_id = second.run_id
        client.wait_run(second_run_id, timeout=60)
        second_output = wait_sealed_outputs(
            client, second_run_id, ["/tmp/second-output.txt"]
        )["/tmp/second-output.txt"]
        downloaded = BytesIO()
        client.download_sealed_output(
            second_run_id, second_output.output_id, downloaded, timeout=60
        )
        if downloaded.getvalue() != b"complete":
            raise RuntimeError("second independent Run returned an invalid output")
        handshake.joinpath("python.run-id").write_text(second_run_id, encoding="utf-8")
        wait_verified(handshake / "python.verified")
        print(
            f"sdk_data_plane=python first_run_id={run_id} "
            f"second_run_id={second_run_id} sealed_output=true ok=true"
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
    client: AxernClient, run_id: str, expected_outputs: list[DeclaredOutput]
) -> None:
    run = client.get_run(run_id, timeout=10)
    format_values = {
        DeclaredOutputFormat.FILE: common_pb2.DECLARED_OUTPUT_FORMAT_FILE,
        DeclaredOutputFormat.TAR: common_pb2.DECLARED_OUTPUT_FORMAT_TAR,
    }
    actual = normalized_declared_output_contract(
        run.config.declared_outputs,
        format_number=int,
    )
    expected = normalized_declared_output_contract(
        expected_outputs,
        format_number=format_values.__getitem__,
    )
    if actual != expected:
        raise RuntimeError(
            "Run did not preserve its declared-output contract: "
            f"expected={expected!r} actual={actual!r}"
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


def assert_output_archive(payload: bytes, marker: str) -> None:
    with tarfile.open(fileobj=BytesIO(payload), mode="r:") as archive:
        members = [member for member in archive.getmembers() if member.isfile()]
        if len(members) != 1 or Path(members[0].name).name != "log.txt":
            raise RuntimeError(f"unexpected declared-output archive: {members!r}")
        extracted = archive.extractfile(members[0])
        if extracted is None or extracted.read().decode().strip() != marker:
            raise RuntimeError("declared-output archive did not preserve its file")


def required(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} is required")
    return value


if __name__ == "__main__":
    main()
