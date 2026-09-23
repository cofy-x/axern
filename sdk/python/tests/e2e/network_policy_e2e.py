"""Public-SDK truth test for deny-all versus unrestricted HTTPS egress."""

from __future__ import annotations

import argparse
import hashlib
import io
import json
import time

from axern.control.common.v1 import common_pb2
from axern.control.run.v1 import run_pb2
from axern_sdk import AxernClient, DeclaredOutput, DeclaredOutputFormat, NetworkPolicy, SandboxError


PROBE = """import json, os, socket, ssl, urllib.request
url = {url}
ca_pem = {ca_pem}
direct_host = {direct_host}
direct_port = {direct_port}
try:
    context = ssl.create_default_context(cadata=ca_pem) if ca_pem else None
    with urllib.request.urlopen(url, timeout=10, context=context) as response:
        permitted = response.status == 200
except OSError:
    permitted = False
try:
    with socket.create_connection((direct_host, direct_port), timeout=5):
        direct_tcp = True
except OSError:
    direct_tcp = False
os.makedirs('/outputs', exist_ok=True)
with open('/outputs/network.json', 'w', encoding='utf-8') as stream:
    json.dump({{'https_200': permitted, 'direct_tcp': direct_tcp}}, stream)
"""


def probe(
    client: AxernClient,
    environment_id: str,
    run_ids: list[str],
    *,
    deny_all: bool,
    command: str,
) -> dict[str, object]:
    policy = NetworkPolicy.deny_all() if deny_all else None
    run = client.create_run(
        environment_id=environment_id,
        argv=["/usr/local/bin/python", "-c", command],
        network_policy=policy,
        declared_outputs=[DeclaredOutput("/outputs/network.json", DeclaredOutputFormat.FILE, "application/json")],
    )
    run_ids.append(run.id)
    terminal = client.wait_run(run.id, timeout=180)
    if terminal.status != run_pb2.RUN_STATUS_SUCCEEDED or not terminal.HasField("exit_code") or terminal.exit_code != 0:
        raise RuntimeError(f"Run {run.id} failed before HTTPS result: {terminal.status} / {terminal.diagnostic_code}")
    if not terminal.allocation_id:
        raise RuntimeError(f"Run {run.id} has no Allocation")
    if deny_all and terminal.config.network.mode != common_pb2.NETWORK_MODE_ISOLATED:
        raise RuntimeError("deny-all was not persisted as isolated network mode")

    deadline = time.monotonic() + 45
    while True:
        try:
            manifest = client.get_sealed_output_manifest(run.id)
        except SandboxError as error:
            if getattr(error, "code", "") not in {"NOT_FOUND", "UNAVAILABLE"}:
                raise
            manifest = []
        if len(manifest) == 1 and manifest[0].status == "available":
            break
        if any(output.status not in {"pending", "available"} for output in manifest):
            raise RuntimeError(f"Run {run.id} output sealing failed")
        if time.monotonic() >= deadline:
            raise RuntimeError(f"Run {run.id} output sealing did not complete")
        time.sleep(0.2)
    output = io.BytesIO()
    client.download_sealed_output(run.id, manifest[0].output_id, output)
    downloaded = output.getvalue()
    if hashlib.sha256(downloaded).hexdigest() != manifest[0].sha256:
        raise RuntimeError(f"Run {run.id} sealed output digest does not match downloaded bytes")
    payload = json.loads(downloaded)
    if type(payload.get("https_200")) is not bool or type(payload.get("direct_tcp")) is not bool:
        raise RuntimeError(f"Run {run.id} produced an invalid HTTPS result")
    return {
        "run_id": run.id,
        "allocation_id": terminal.allocation_id,
        "https_200": payload["https_200"],
        "direct_tcp": payload["direct_tcp"],
        "sealed_sha256": manifest[0].sha256,
    }


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--context-config")
    parser.add_argument("--endpoint")
    parser.add_argument("--tls-ca-cert")
    parser.add_argument("--tls-cert")
    parser.add_argument("--tls-key")
    parser.add_argument("--image-ref", required=True)
    parser.add_argument("--https-url", default="https://example.com/")
    parser.add_argument("--tls-ca-pem-file")
    parser.add_argument("--direct-tcp-host", default="1.1.1.1")
    parser.add_argument("--direct-tcp-port", type=int, default=443)
    args = parser.parse_args()
    if bool(args.context_config) == bool(args.endpoint):
        parser.error("provide exactly one of --context-config or --endpoint")
    if args.endpoint and not all((args.tls_ca_cert, args.tls_cert, args.tls_key)):
        parser.error("--endpoint requires a complete mTLS identity")
    if not args.https_url.startswith("https://"):
        parser.error("--https-url must use HTTPS")

    ca_pem = ""
    if args.tls_ca_pem_file:
        with open(args.tls_ca_pem_file, encoding="utf-8") as certificate:
            ca_pem = certificate.read()
    command = PROBE.format(
        url=json.dumps(args.https_url),
        ca_pem=json.dumps(ca_pem),
        direct_host=json.dumps(args.direct_tcp_host),
        direct_port=args.direct_tcp_port,
    )

    client = (
        AxernClient.from_context(args.context_config)
        if args.context_config
        else AxernClient(
            args.endpoint,
            tls_ca_cert=args.tls_ca_cert,
            tls_cert=args.tls_cert,
            tls_key=args.tls_key,
            proxy_mode="direct",
        )
    )
    environment_id = ""
    run_ids: list[str] = []
    try:
        environment_id = client.create_environment(image_ref=args.image_ref).id
        results = []
        for deny_all in (True, False):
            result = probe(client, environment_id, run_ids, deny_all=deny_all, command=command)
            results.append(result)
        if results[0]["run_id"] == results[1]["run_id"] or results[0]["allocation_id"] == results[1]["allocation_id"]:
            raise RuntimeError("policy trials reused a Run or Allocation")
        print(json.dumps({"results": results}, sort_keys=True), flush=True)
        if results[0]["https_200"] is not False or results[0]["direct_tcp"] is not False or results[1]["https_200"] is not True:
            raise RuntimeError("deny-all permitted HTTPS or unrestricted could not reach HTTPS")
    finally:
        for run_id in run_ids:
            current = client.get_run(run_id)
            if current.status not in {run_pb2.RUN_STATUS_SUCCEEDED, run_pb2.RUN_STATUS_FAILED, run_pb2.RUN_STATUS_CANCELLED}:
                client.cancel_run(run_id)
        if environment_id:
            client.delete_environment(environment_id)
            print(f"deleted_environment={environment_id}", flush=True)
        client.close()


if __name__ == "__main__":
    main()
