"""Compose E2E for the Python SDK Sandbox tunnel flow."""

from __future__ import annotations

import argparse
import asyncio
import hashlib
import ipaddress
import json
import os
import subprocess
import sys
import threading
import time
from http.server import SimpleHTTPRequestHandler, ThreadingHTTPServer
from io import BytesIO
from pathlib import Path
from tempfile import TemporaryDirectory

os.environ.setdefault("GRPC_VERBOSITY", "ERROR")
os.environ.setdefault("GLOG_minloglevel", "2")

from axern.control.common.v1 import common_pb2
from axern.control.run.v1 import run_pb2
from axern.control.tunnel.v1 import tunnel_pb2
from axern_sdk import (
    AsyncAxernClient,
    AsyncSandbox,
    AxernClient,
    DeclaredOutput,
    DeclaredOutputFormat,
    NetworkPolicy,
    Sandbox,
    SandboxError,
    TunnelConnector,
)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--endpoint", required=True)
    parser.add_argument("--tls-ca-cert", required=True)
    parser.add_argument("--tls-cert", required=True)
    parser.add_argument("--tls-key", required=True)
    parser.add_argument("--image-ref", required=True)
    parser.add_argument("--tls-server-name", default="")
    parser.add_argument("--node-container", required=True)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    marker = f"axern-python-sdk-sandbox-ok-{int(time.time())}"
    with TemporaryDirectory() as tmp:
        root = Path(tmp)
        (root / "index.txt").write_text(marker + "\n")
        large_payload = bytes(range(256)) * 8192 + b"tunnel-end"
        (root / "large.bin").write_bytes(large_payload)
        large_payload_sha256 = hashlib.sha256(large_payload).hexdigest()
        server = ThreadingHTTPServer(("127.0.0.1", 0), _handler_for(root))
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        upstream = f"127.0.0.1:{server.server_port}"
        client = AxernClient(
            args.endpoint,
            tls_ca_cert=args.tls_ca_cert,
            tls_cert=args.tls_cert,
            tls_key=args.tls_key,
            tls_server_name=args.tls_server_name,
        )
        run_id = ""
        session_id = ""
        phase = "sync-start"
        try:
            with Sandbox(
                client=client,
                image=args.image_ref,
                argv=["python", "-c", "import time; time.sleep(600)"],
                upstream=upstream,
                ready_timeout_seconds=180,
            ) as sandbox:
                phase = "sync-egress-control"
                bridge_ip = node_bridge_address(args.node_container)
                if (
                    client.get_run(sandbox.run_id).config.network.mode
                    == common_pb2.NETWORK_MODE_ISOLATED
                ):
                    raise RuntimeError(
                        "control Sandbox unexpectedly used isolated network"
                    )
                egress_control = sandbox.exec(
                    [
                        "python",
                        "-c",
                        "import sys,urllib.request\n"
                        "opener=urllib.request.build_opener(urllib.request.ProxyHandler({}))\n"
                        "with opener.open(sys.argv[1],timeout=5) as response:\n"
                        "    print(response.status)\n",
                        f"http://{bridge_ip}:23001/readyz",
                    ],
                    timeout_seconds=10,
                    check=True,
                    text=True,
                )
                if egress_control.stdout.strip() != "200":
                    raise RuntimeError(
                        "control Sandbox could not reach node bridge HTTP service"
                    )
                phase = "sync-file-api"
                if not sandbox.read_file("/etc/hostname").strip():
                    raise SystemExit("sandbox read_file returned an empty hostname")
                sandbox.write_file("/tmp/axern-sdk-v2.txt", "file-api-ok\n")
                if sandbox.read_file("/tmp/axern-sdk-v2.txt") != "file-api-ok\n":
                    raise SystemExit("sandbox file API round trip failed")
                upload_source = root / "upload.bin"
                download_target = root / "downloaded" / "upload.bin"
                upload_source.write_bytes(b"\x00axern-upload\xff")
                sandbox.upload_file(upload_source, "/tmp/axern-sdk-upload.bin")
                sandbox.download_file("/tmp/axern-sdk-upload.bin", download_target)
                if download_target.read_bytes() != upload_source.read_bytes():
                    raise SystemExit("sandbox upload/download file round trip failed")
                upload_dir = root / "upload-tree"
                upload_dir.joinpath("nested").mkdir(parents=True)
                upload_dir.joinpath("nested", "data.txt").write_text("archive-ok\n")
                upload_dir.joinpath("empty").mkdir()
                download_dir = root / "download-tree"
                sandbox.upload_dir(upload_dir, "/tmp/axern-sdk-upload-tree")
                sandbox.download_dir("/tmp/axern-sdk-upload-tree", download_dir)
                if (
                    download_dir.joinpath("nested", "data.txt").read_text()
                    != "archive-ok\n"
                ):
                    raise SystemExit("sandbox upload/download dir round trip failed")
                if not sandbox.exists("/tmp/axern-sdk-v2.txt"):
                    raise SystemExit("sandbox exists returned false for a written file")
                if sandbox.stat("/tmp/axern-sdk-v2.txt").size != len("file-api-ok\n"):
                    raise SystemExit("sandbox stat returned an unexpected file size")
                if not any(
                    entry.path == "/tmp/axern-sdk-v2.txt"
                    for entry in sandbox.list_dir("/tmp")
                ):
                    raise SystemExit("sandbox list_dir did not include a written file")
                file_process = sandbox.exec(
                    [
                        "python",
                        "-c",
                        "from pathlib import Path; print(Path('/tmp/axern-sdk-v2.txt').read_text().strip().upper())",
                    ],
                    timeout_seconds=15,
                    text=True,
                )
                if file_process.stdout.strip() != "FILE-API-OK":
                    raise SystemExit(
                        "sandbox file/process round trip returned unexpected stdout"
                    )
                sandbox.copy(
                    "/tmp/axern-sdk-v2.txt",
                    "/tmp/axern-sdk-v2-copy.txt",
                    overwrite=True,
                )
                sandbox.move(
                    "/tmp/axern-sdk-v2-copy.txt",
                    "/tmp/axern-sdk-v2-moved.txt",
                    overwrite=True,
                )
                sandbox.chmod("/tmp/axern-sdk-v2-moved.txt", 0o600)
                sandbox.touch("/tmp/axern-sdk-v2-moved.txt")
                sandbox.mkdir("/tmp/axern-sdk-v2-dir")
                sandbox.remove("/tmp/axern-sdk-v2-dir", recursive=True, force=True)
                if sandbox.exists("/tmp/axern-sdk-v2-dir"):
                    raise SystemExit("sandbox remove left the directory behind")
                phase = "sync-process"
                with sandbox.process(
                    [
                        "/bin/sh",
                        "-lc",
                        "cat | tr '[:lower:]' '[:upper:]'; printf 'process-err' >&2; exit 3",
                    ],
                    timeout_seconds=15,
                ) as process:
                    process.write("process-ok\n")
                    process.close_stdin()
                    process_events = list(process.events())
                if (
                    b"".join(
                        event.data
                        for event in process_events
                        if event.stream == "stdout"
                    ).strip()
                    != b"PROCESS-OK"
                ):
                    raise SystemExit(
                        f"sandbox process returned unexpected stdout: {process_events!r}"
                    )
                if (
                    b"".join(
                        event.data
                        for event in process_events
                        if event.stream == "stderr"
                    )
                    != b"process-err"
                ):
                    raise SystemExit(
                        f"sandbox process returned unexpected stderr: {process_events!r}"
                    )
                if process_events[-1].exit_code != 3:
                    raise SystemExit(
                        f"sandbox process exit code = {process_events[-1].exit_code}, want 3"
                    )
                phase = "sync-exec-stream"
                stream_events = list(
                    sandbox.exec_stream(
                        ["python", "-c", "print('stream-ok')"], timeout_seconds=15
                    )
                )
                if (
                    b"".join(
                        event.data
                        for event in stream_events
                        if event.stream == "stdout"
                    ).strip()
                    != b"stream-ok"
                ):
                    raise SystemExit("sandbox exec_stream returned unexpected stdout")
                if stream_events[-1].exit_code != 0:
                    raise SystemExit(
                        f"sandbox exec_stream exit code = {stream_events[-1].exit_code}, want 0"
                    )
                code = f"""
import sys
import urllib.request

with urllib.request.urlopen("http://{sandbox.bound_addr}/index.txt", timeout=5) as response:
    sys.stdout.write(response.read().decode().strip())
"""
                phase = "sync-tunnel-request"
                result = sandbox.exec(
                    ["python", "-c", code], timeout_seconds=15, check=True
                )
                observed = result.stdout_text().strip()
                if observed != marker:
                    raise SystemExit(
                        f"unexpected sandbox tunnel response: {observed!r}"
                    )
                events = client.list_tunnel_events(sandbox.tunnel_session_id, limit=50)
                require_event(
                    events, tunnel_pb2.TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED
                )
                require_event(
                    events, tunnel_pb2.TUNNEL_SESSION_EVENT_TYPE_NODE_CONNECTED
                )
                require_event(events, tunnel_pb2.TUNNEL_SESSION_EVENT_TYPE_PAIRED)
                session_id = sandbox.tunnel_session_id
                run_id = sandbox.run_id
            phase = "sync-cleanup"
            session = client.get_tunnel_session(session_id)
            if session.status != tunnel_pb2.TUNNEL_SESSION_STATUS_REVOKED:
                raise SystemExit(
                    f"tunnel status after sandbox close = {session.status}, want revoked"
                )
            print(
                "python_sdk_sandbox_tunnel_e2e_ok=true "
                f"run_id={run_id} session_id={session_id}"
            )
            phase = "low-level-loopback-service"
            run_low_level_loopback_service_check(
                client, args.image_ref, upstream, marker
            )
            phase = "isolated-tunnel"
            run_isolated_tunnel_check(
                client, args, upstream, marker, bridge_ip, large_payload_sha256
            )
            phase = "async-check"
            asyncio.run(run_async_sandbox_check(args))
            return 0
        except BaseException as exc:
            if not getattr(exc, "_axern_e2e_logged", False):
                log_e2e_failure(
                    args, phase=phase, run_id=run_id, session_id=session_id, exc=exc
                )
            raise
        finally:
            client.close()
            server.shutdown()
            server.server_close()
            thread.join()


def _handler_for(root: Path):
    class Handler(SimpleHTTPRequestHandler):
        def __init__(self, *args, **kwargs) -> None:
            super().__init__(*args, directory=str(root), **kwargs)

        def log_message(self, *_args) -> None:
            return

    return Handler


def run_low_level_loopback_service_check(
    client: AxernClient,
    image_ref: str,
    upstream: str,
    marker: str,
) -> None:
    """Exercise a client readiness barrier using only public SDK APIs."""

    environment = client.create_environment(
        image_ref=image_ref,
        labels={"axern.e2e": "loopback-service"},
    )
    run_id = ""
    session_id = ""
    connector: TunnelConnector | None = None
    try:
        workload = """
import time
import urllib.request
from pathlib import Path

root = Path('/run/client-inputs')
while not root.joinpath('inputs-ready').exists():
    time.sleep(0.05)
config = dict(
    line.split('=', 1)
    for line in root.joinpath('service.env').read_text().splitlines()
    if line
)
with urllib.request.urlopen(config['SERVICE_ENDPOINT'] + '/index.txt', timeout=5) as response:
    Path('/tmp/output.txt').write_bytes(response.read())
while not root.joinpath('finish').exists():
    time.sleep(0.05)
"""
        run = client.create_run(
            environment_id=environment.id,
            argv=["python", "-c", workload],
            declared_outputs=[
                DeclaredOutput(
                    "/tmp/output.txt",
                    DeclaredOutputFormat.FILE,
                    "text/plain",
                )
            ],
            labels={"axern.e2e": "loopback-service"},
        )
        run_id = run.id
        run = wait_for_allocation(client, run.id)
        allocation = client.allocation(run.allocation_id)
        tunnel = client.create_tunnel_session(
            allocation_id=run.allocation_id,
            ttl_seconds=45,
            wait_ready=True,
            ready_timeout_seconds=30,
        )
        session_id = tunnel.session.session_id
        connector = TunnelConnector(
            client=client,
            session=tunnel.session,
            client_token=tunnel.client_token,
            local_target=upstream,
        )
        connector.start()
        wait_for_tunnel_client(client, session_id)

        allocation.mkdir("/run/client-inputs", parents=True)
        endpoint = (
            tunnel.session.bound_addr or f"127.0.0.1:{tunnel.session.remote_port}"
        )
        allocation.write_file(
            "/run/client-inputs/service.env",
            f"SERVICE_ENDPOINT=http://{endpoint}\n".encode(),
        )
        allocation.write_file("/run/client-inputs/input.txt", b"non-sensitive input\n")
        allocation.write_file("/run/client-inputs/inputs-ready", b"ready\n")
        wait_for_file(allocation, "/tmp/output.txt")

        client.revoke_tunnel_session(session_id, reason="service access complete")
        if not connector.wait_closed(25):
            raise RuntimeError(
                "tunnel connector did not close within the revocation bound"
            )
        run_after_revoke = client.get_run(run_id)
        if run_after_revoke.status != run_pb2.RUN_STATUS_RUNNING:
            raise RuntimeError("Tunnel revoke unexpectedly terminated the Run")
        denied = allocation.exec(
            [
                "python",
                "-c",
                "import sys,urllib.request; "
                f"url='http://{endpoint}/index.txt'; "
                "\ntry: urllib.request.urlopen(url, timeout=2); sys.exit(2)"
                "\nexcept Exception: print('revoked')",
            ],
            timeout_seconds=10,
            check=True,
            text=True,
        )
        if denied.stdout.strip() != "revoked":
            raise RuntimeError("revoked Tunnel still accepted a new request")

        allocation.write_file("/run/client-inputs/finish", b"finish\n")
        terminal = client.wait_run(run_id, timeout=60)
        if terminal.status != run_pb2.RUN_STATUS_SUCCEEDED:
            raise RuntimeError("loopback-service Run did not succeed")
        output = download_only_output(client, run_id)
        if output.decode().strip() != marker:
            raise RuntimeError("declared output content is invalid")

        check_fresh_run(client, environment.id, output)
        print(
            "python_sdk_loopback_service_e2e_ok=true "
            f"run_id={run_id} session_id={session_id}"
        )
        run_id = ""
        session_id = ""
    finally:
        if connector is not None:
            connector.stop()
        if session_id:
            try:
                client.revoke_tunnel_session(session_id, reason="test cleanup")
            except Exception:
                pass
        if run_id:
            try:
                client.cancel_run(run_id)
            except Exception:
                pass
        client.delete_environment(environment.id)


ISOLATED_TUNNEL_PROBE = """
import errno
import hashlib
import json
import socket
import ssl
import sys
import urllib.error
import urllib.request

bridge_ip, tunnel_port, marker, expected_hash = sys.argv[1], int(sys.argv[2]), sys.argv[3], sys.argv[4]
blocked_errnos = {errno.ENETUNREACH, errno.EHOSTUNREACH, errno.ETIMEDOUT, errno.EACCES, errno.EPERM}
opener = urllib.request.build_opener(
    urllib.request.ProxyHandler({}),
    urllib.request.HTTPSHandler(context=ssl.create_default_context()),
)

def network_blocked(error):
    if isinstance(error, urllib.error.URLError):
        error = error.reason
    return isinstance(error, socket.gaierror) or isinstance(error, TimeoutError) or (
        isinstance(error, OSError) and error.errno in blocked_errnos
    )

def blocked_https(url):
    try:
        with opener.open(url, timeout=4) as response:
            response.read(1)
    except OSError as error:
        return network_blocked(error)
    return False

def blocked_tcp(host, port):
    try:
        with socket.create_connection((host, port), timeout=4):
            return False
    except OSError as error:
        return network_blocked(error)

with opener.open(f'http://127.0.0.1:{tunnel_port}/index.txt', timeout=10) as response:
    tunnel_body = response.read().decode().strip()
with opener.open(f'http://127.0.0.1:{tunnel_port}/large.bin', timeout=30) as response:
    large_hash = hashlib.sha256(response.read()).hexdigest()

try:
    socket.getaddrinfo('example.com', 443, type=socket.SOCK_STREAM)
    dns_blocked = False
except socket.gaierror:
    dns_blocked = True

print(json.dumps({
    'tunnel_ok': tunnel_body == marker,
    'large_response_intact': large_hash == expected_hash,
    'dns_blocked': dns_blocked,
    'external_https_blocked': blocked_https('https://example.com/'),
    'bridge_https_blocked': blocked_https(f'https://{bridge_ip}:23001/readyz'),
    'direct_ip_blocked': blocked_tcp(bridge_ip, 23001),
}, sort_keys=True))
"""


def node_bridge_address(node_container: str) -> str:
    result = subprocess.run(
        [
            "docker",
            "exec",
            node_container,
            "ip",
            "-4",
            "-o",
            "addr",
            "show",
            "dev",
            "sandbox0",
        ],
        check=True,
        capture_output=True,
        text=True,
        timeout=10,
    )
    fields = result.stdout.split()
    if len(fields) < 4 or fields[2] != "inet":
        raise RuntimeError("node sandbox bridge has no IPv4 address")
    address = str(ipaddress.IPv4Interface(fields[3]).ip)
    ready = subprocess.run(
        [
            "docker",
            "exec",
            node_container,
            "curl",
            "--noproxy",
            "*",
            "--silent",
            "--show-error",
            "--fail",
            "--max-time",
            "3",
            f"http://{address}:23001/readyz",
        ],
        capture_output=True,
        text=True,
        timeout=10,
    )
    if ready.returncode != 0:
        raise RuntimeError(
            "node sandbox bridge HTTP target is not reachable for egress probe"
        )
    return address


def run_isolated_tunnel_check(
    client: AxernClient,
    args: argparse.Namespace,
    upstream: str,
    marker: str,
    bridge_ip: str,
    large_payload_sha256: str,
) -> None:
    """Keep deny-all egress while the declared loopback Tunnel is live."""

    environment = client.create_environment(
        image_ref=args.image_ref,
        labels={"axern.e2e": "isolated-tunnel"},
    )
    run_id = ""
    session_id = ""
    connector: TunnelConnector | None = None
    try:
        run = client.create_run(
            environment_id=environment.id,
            argv=[
                "python",
                "-c",
                "import time\n"
                "from pathlib import Path\n"
                "while not Path('/tmp/isolated-finish').exists():\n"
                "    time.sleep(0.05)\n"
                "Path('/tmp/isolated-output.txt').write_text('isolated-output-ok\\n')\n",
            ],
            network_policy=NetworkPolicy.deny_all(),
            declared_outputs=[
                DeclaredOutput(
                    "/tmp/isolated-output.txt",
                    DeclaredOutputFormat.FILE,
                    "text/plain",
                )
            ],
            labels={"axern.e2e": "isolated-tunnel"},
        )
        run_id = run.id
        run = wait_for_allocation(client, run_id)
        if run.config.network.mode != common_pb2.NETWORK_MODE_ISOLATED:
            raise RuntimeError("deny-all Run was not persisted as isolated")
        allocation = client.allocation(run.allocation_id)
        tunnel = client.create_tunnel_session(
            allocation_id=run.allocation_id,
            remote_port=8765,
            ttl_seconds=300,
            wait_ready=True,
            ready_timeout_seconds=45,
        )
        session_id = tunnel.session.session_id
        if (
            tunnel.session.remote_port != 8765
            or tunnel.session.bound_addr != "127.0.0.1:8765"
        ):
            raise RuntimeError(
                "Tunnel did not bind the requested sandbox loopback port"
            )
        connector = TunnelConnector(
            client=client,
            session=tunnel.session,
            client_token=tunnel.client_token,
            local_target=upstream,
        )
        connector.start()
        wait_for_tunnel_event(
            client, session_id, tunnel_pb2.TUNNEL_SESSION_EVENT_TYPE_PAIRED
        )
        result = allocation.exec(
            [
                "python",
                "-c",
                ISOLATED_TUNNEL_PROBE,
                bridge_ip,
                "8765",
                marker,
                large_payload_sha256,
            ],
            timeout_seconds=60,
            check=True,
        )
        observed = json.loads(result.stdout_text())
        expected = {
            "tunnel_ok",
            "large_response_intact",
            "dns_blocked",
            "external_https_blocked",
            "bridge_https_blocked",
            "direct_ip_blocked",
        }
        if set(observed) != expected or not all(
            value is True for value in observed.values()
        ):
            raise RuntimeError(f"isolated Tunnel or egress probe failed: {observed}")

        client.revoke_tunnel_session(session_id, reason="isolated tunnel test complete")
        if not connector.wait_closed(25):
            raise RuntimeError(
                "isolated Tunnel connector did not close after revocation"
            )
        session = client.get_tunnel_session(session_id)
        if session.status != tunnel_pb2.TUNNEL_SESSION_STATUS_REVOKED:
            raise RuntimeError("isolated Tunnel was not revoked")
        if client.get_run(run_id).status != run_pb2.RUN_STATUS_RUNNING:
            raise RuntimeError("Tunnel revocation ended the isolated Run")
        revoked = allocation.exec(
            [
                "python",
                "-c",
                "import errno,socket,sys,time\n"
                "deadline=time.monotonic()+15\n"
                "while time.monotonic()<deadline:\n"
                "    try:\n"
                "        with socket.create_connection(('127.0.0.1',8765),timeout=2): pass\n"
                "    except OSError as error:\n"
                "        if error.errno in {errno.ECONNREFUSED,errno.ECONNRESET}:\n"
                "            print('revoked'); sys.exit(0)\n"
                "    time.sleep(0.2)\n"
                "sys.exit(1)\n",
            ],
            timeout_seconds=25,
            check=True,
            text=True,
        )
        if revoked.stdout.strip() != "revoked":
            raise RuntimeError("revoked Tunnel still accepted a sandbox request")
        allocation.write_file("/tmp/isolated-finish", b"finish\n")
        terminal = client.wait_run(run_id, timeout=60)
        if terminal.status != run_pb2.RUN_STATUS_SUCCEEDED:
            raise RuntimeError("isolated Run did not finish cleanly")
        if download_only_output(client, run_id) != b"isolated-output-ok\n":
            raise RuntimeError("isolated Run declared output was not sealed")
        print(
            f"python_sdk_isolated_tunnel_e2e_ok=true run_id={run_id} session_id={session_id}"
        )
        run_id = ""
        session_id = ""
    except BaseException as exc:
        log_e2e_failure(
            args, phase="isolated-tunnel", run_id=run_id, session_id=session_id, exc=exc
        )
        raise
    finally:
        if connector is not None:
            connector.stop()
        if session_id:
            try:
                client.revoke_tunnel_session(session_id, reason="isolated test cleanup")
            except Exception:
                pass
        if run_id:
            try:
                client.cancel_run(run_id)
            except Exception:
                pass
        client.delete_environment(environment.id)


def wait_for_allocation(client: AxernClient, run_id: str):
    for run in client.watch_run(run_id, timeout=180):
        if run.status == run_pb2.RUN_STATUS_RUNNING and run.allocation_id:
            return run
        if run.status in {
            run_pb2.RUN_STATUS_SUCCEEDED,
            run_pb2.RUN_STATUS_FAILED,
            run_pb2.RUN_STATUS_CANCELLED,
        }:
            raise RuntimeError("Run terminated before Allocation binding")
    raise RuntimeError("Run watch ended before Allocation binding")


def wait_for_tunnel_client(client: AxernClient, session_id: str) -> None:
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        events = client.list_tunnel_events(session_id, limit=50)
        if any(
            event.event_type == tunnel_pb2.TUNNEL_SESSION_EVENT_TYPE_CLIENT_CONNECTED
            for event in events
        ):
            return
        time.sleep(0.1)
    raise RuntimeError("Tunnel client did not connect")


def wait_for_tunnel_event(
    client: AxernClient, session_id: str, event_type: int
) -> None:
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        events = client.list_tunnel_events(session_id, limit=50)
        if any(event.event_type == event_type for event in events):
            return
        time.sleep(0.1)
    raise RuntimeError(
        f"Tunnel did not report {tunnel_pb2.TunnelSessionEventType.Name(event_type)}"
    )


def wait_for_file(allocation, path: str) -> None:
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        if allocation.exists(path):
            return
        time.sleep(0.1)
    raise RuntimeError(f"Allocation did not produce {path}")


def download_only_output(client: AxernClient, run_id: str) -> bytes:
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        try:
            outputs = client.get_sealed_output_manifest(run_id)
        except SandboxError as error:
            if getattr(error, "code", "") not in {"NOT_FOUND", "UNAVAILABLE"}:
                raise
            time.sleep(0.1)
            continue
        if len(outputs) == 1 and outputs[0].status == "available":
            destination = BytesIO()
            client.download_sealed_output(run_id, outputs[0].output_id, destination)
            return destination.getvalue()
        time.sleep(0.1)
    raise RuntimeError("declared output was not sealed")


def check_fresh_run(
    client: AxernClient,
    environment_id: str,
    output: bytes,
) -> None:
    with Sandbox(
        client=client,
        environment_id=environment_id,
        argv=["python", "-c", "import time; time.sleep(600)"],
    ) as fresh:
        fresh_run = client.get_run(fresh.run_id)
        if (
            fresh_run.config.image_mounts
            or fresh_run.config.secret_env
            or fresh_run.config.secret_files
        ):
            raise RuntimeError("fresh Run inherited earlier Run projections")
        if fresh.tunnel_session_id:
            raise RuntimeError("fresh Run inherited earlier TunnelSession")
        fresh.write_file("/tmp/input.txt", output)
        result = fresh.exec(
            [
                "python",
                "-c",
                "from pathlib import Path; "
                "assert Path('/tmp/input.txt').read_text().strip()",
            ],
            check=True,
        )
        if result.exit_code != 0:
            raise RuntimeError("fresh Run rejected input")


async def run_async_sandbox_check(args: argparse.Namespace) -> None:
    run_id = ""
    phase = "async-start"
    try:
        async with AsyncAxernClient(
            args.endpoint,
            tls_ca_cert=args.tls_ca_cert,
            tls_cert=args.tls_cert,
            tls_key=args.tls_key,
            tls_server_name=args.tls_server_name,
        ) as client:
            async with AsyncSandbox(
                client=client,
                image=args.image_ref,
                argv=["python", "-c", "import time; time.sleep(600)"],
                ready_timeout_seconds=180,
            ) as sandbox:
                run_id = sandbox.run_id
                phase = "async-exec"
                result = await sandbox.exec(
                    ["python", "-c", "print('async-ok')"],
                    timeout_seconds=15,
                    check=True,
                )
                if result.stdout_text().strip() != "async-ok":
                    raise SystemExit(
                        f"unexpected async sandbox exec response: {result.stdout_text()!r}"
                    )
                if not (await sandbox.read_file("/etc/hostname")).strip():
                    raise SystemExit(
                        "async sandbox read_file returned an empty hostname"
                    )
                phase = "async-file-api"
                await sandbox.write_file("/tmp/axern-sdk-async.txt", "async-file-ok\n")
                if (
                    await sandbox.read_file("/tmp/axern-sdk-async.txt")
                    != "async-file-ok\n"
                ):
                    raise SystemExit("async sandbox file API round trip failed")
                with TemporaryDirectory() as tmp:
                    root = Path(tmp)
                    upload_source = root / "async-upload.bin"
                    download_target = root / "downloaded" / "async-upload.bin"
                    upload_source.write_bytes(b"\x00axern-async-upload\xff")
                    await sandbox.upload_file(
                        upload_source, "/tmp/axern-sdk-async-upload.bin"
                    )
                    await sandbox.download_file(
                        "/tmp/axern-sdk-async-upload.bin", download_target
                    )
                    if download_target.read_bytes() != upload_source.read_bytes():
                        raise SystemExit(
                            "async sandbox upload/download file round trip failed"
                        )
                    upload_dir = root / "async-upload-tree"
                    upload_dir.joinpath("nested").mkdir(parents=True)
                    upload_dir.joinpath("nested", "data.txt").write_text(
                        "async-archive-ok\n"
                    )
                    download_dir = root / "async-download-tree"
                    await sandbox.upload_dir(
                        upload_dir, "/tmp/axern-sdk-async-upload-tree"
                    )
                    await sandbox.download_dir(
                        "/tmp/axern-sdk-async-upload-tree", download_dir
                    )
                    if (
                        download_dir.joinpath("nested", "data.txt").read_text()
                        != "async-archive-ok\n"
                    ):
                        raise SystemExit(
                            "async sandbox upload/download dir round trip failed"
                        )
                if not await sandbox.exists("/tmp/axern-sdk-async.txt"):
                    raise SystemExit(
                        "async sandbox exists returned false for a written file"
                    )
                if (await sandbox.stat("/tmp/axern-sdk-async.txt")).size != len(
                    "async-file-ok\n"
                ):
                    raise SystemExit(
                        "async sandbox stat returned an unexpected file size"
                    )
                if not any(
                    entry.path == "/tmp/axern-sdk-async.txt"
                    for entry in await sandbox.list_dir("/tmp")
                ):
                    raise SystemExit(
                        "async sandbox list_dir did not include a written file"
                    )
                async_file_process = await sandbox.exec(
                    [
                        "python",
                        "-c",
                        "from pathlib import Path; print(Path('/tmp/axern-sdk-async.txt').read_text().strip().upper())",
                    ],
                    timeout_seconds=15,
                    text=True,
                )
                if async_file_process.stdout.strip() != "ASYNC-FILE-OK":
                    raise SystemExit(
                        "async sandbox file/process round trip returned unexpected stdout"
                    )
                await sandbox.copy(
                    "/tmp/axern-sdk-async.txt",
                    "/tmp/axern-sdk-async-copy.txt",
                    overwrite=True,
                )
                await sandbox.move(
                    "/tmp/axern-sdk-async-copy.txt",
                    "/tmp/axern-sdk-async-moved.txt",
                    overwrite=True,
                )
                await sandbox.chmod("/tmp/axern-sdk-async-moved.txt", 0o600)
                await sandbox.touch("/tmp/axern-sdk-async-moved.txt")
                await sandbox.mkdir("/tmp/axern-sdk-async-dir")
                await sandbox.remove(
                    "/tmp/axern-sdk-async-dir", recursive=True, force=True
                )
                if await sandbox.exists("/tmp/axern-sdk-async-dir"):
                    raise SystemExit("async sandbox remove left the directory behind")
                phase = "async-process"
                async with await sandbox.process(
                    [
                        "/bin/sh",
                        "-lc",
                        "cat | tr '[:lower:]' '[:upper:]'; printf 'async-process-err' >&2; exit 3",
                    ],
                    timeout_seconds=15,
                ) as process:
                    await process.write("async-process-ok\n")
                    await process.close_stdin()
                    process_events = [event async for event in process.events()]
                if (
                    b"".join(
                        event.data
                        for event in process_events
                        if event.stream == "stdout"
                    ).strip()
                    != b"ASYNC-PROCESS-OK"
                ):
                    raise SystemExit(
                        f"async sandbox process returned unexpected stdout: {process_events!r}"
                    )
                if (
                    b"".join(
                        event.data
                        for event in process_events
                        if event.stream == "stderr"
                    )
                    != b"async-process-err"
                ):
                    raise SystemExit(
                        f"async sandbox process returned unexpected stderr: {process_events!r}"
                    )
                if process_events[-1].exit_code != 3:
                    raise SystemExit(
                        f"async sandbox process exit code = {process_events[-1].exit_code}, want 3"
                    )
                phase = "async-exec-stream"
                for index in range(5):
                    stream_events = [
                        event
                        async for event in sandbox.exec_stream(
                            ["python", "-c", f"print('async-stream-ok-{index}')"],
                            timeout_seconds=15,
                        )
                    ]
                    expected = f"async-stream-ok-{index}".encode()
                    if (
                        b"".join(
                            event.data
                            for event in stream_events
                            if event.stream == "stdout"
                        ).strip()
                        != expected
                    ):
                        raise SystemExit(
                            "async sandbox exec_stream returned unexpected stdout"
                        )
                    if stream_events[-1].exit_code != 0:
                        raise SystemExit(
                            f"async sandbox exec_stream exit code = {stream_events[-1].exit_code}, want 0"
                        )
    except BaseException as exc:
        log_e2e_failure(args, phase=phase, run_id=run_id, session_id="", exc=exc)
        raise


def log_e2e_failure(
    args: argparse.Namespace,
    *,
    phase: str,
    run_id: str,
    session_id: str,
    exc: BaseException,
) -> None:
    print(
        "python_sdk_sandbox_e2e_failed=true "
        f"phase={phase} "
        f"run_id={run_id or '-'} session_id={session_id or '-'} "
        f"node_container={args.node_container} "
        f"error_type={type(exc).__name__} error={exc}",
        file=sys.stderr,
    )
    setattr(exc, "_axern_e2e_logged", True)


def require_event(events: list[tunnel_pb2.TunnelSessionEvent], event_type: int) -> None:
    if not any(event.event_type == event_type for event in events):
        raise SystemExit(
            f"missing tunnel event {tunnel_pb2.TunnelSessionEventType.Name(event_type)}"
        )


if __name__ == "__main__":
    sys.exit(main())
