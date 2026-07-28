"""Create a service with the Python SDK and reach it through the Axern gateway."""

from __future__ import annotations

import os
import time
import urllib.request

from axern_sdk import AxernClient
from _context import current_context

NAMESPACE = os.environ.get("AXERN_NAMESPACE", "default")
TEMPLATE_ID = os.environ.get("AXERN_TEMPLATE_ID", "python311")
RUNTIME_CLASS = os.environ.get("AXERN_RUNTIME_CLASS", "runc")


def main() -> None:
    context = current_context()
    service_url = os.environ.get("AXERN_SERVICE_URL", context.service_url)
    if not service_url:
        raise SystemExit("current Axern context does not define service_url")
    client = AxernClient.from_context(os.environ.get("AXERN_CONFIG", os.path.expanduser("~/.config/axern/config.json")), context.name)
    environment_id = ""
    service_id = ""
    try:
        environment = client.create_environment(
            namespace=NAMESPACE,
            template_id=TEMPLATE_ID,
            labels={"axern.example": "python-sdk-service-gateway"},
        )
        environment_id = environment.id

        service = client.create_service(
            namespace=NAMESPACE,
            environment_id=environment_id,
            replicas=1,
            argv=["python", "-m", "http.server", "8080", "--bind", "0.0.0.0"],
            runtime_class=RUNTIME_CLASS,
            request_cpu=os.environ.get("AXERN_REQUEST_CPU", "100m"),
            request_memory=os.environ.get("AXERN_REQUEST_MEMORY", "128Mi"),
            limit_cpu=os.environ.get("AXERN_LIMIT_CPU", "500m"),
            limit_memory=os.environ.get("AXERN_LIMIT_MEMORY", "512Mi"),
            labels={"axern.example": "python-sdk-service-gateway"},
        )
        service_id = service.id

        replica = wait_ready_replica(client, service_id)
        url = f"{service_url.rstrip('/')}/svc/{NAMESPACE}/{service_id}/8080/"
        body = fetch(url)
        print(f"service={service_id}")
        print(f"allocation={replica.id} node={replica.node_id}")
        print(body.splitlines()[0])
    finally:
        cleanup(client, service_id, environment_id)
        client.close()


def wait_ready_replica(client: AxernClient, service_id: str, timeout_seconds: float = 180.0):
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        replicas = client.list_service_replicas(service_id, timeout=10.0)
        for replica in replicas:
            if replica.ready:
                return replica
        time.sleep(2)
    raise TimeoutError(f"service {service_id} did not become ready")


def fetch(url: str) -> str:
    with urllib.request.urlopen(url, timeout=20) as response:
        return response.read().decode("utf-8", errors="replace")


def cleanup(client: AxernClient, service_id: str, environment_id: str) -> None:
    if service_id:
        try:
            client.delete_service(service_id, timeout=30.0)
        except Exception as exc:
            print(f"warning: failed to clean service {service_id}: {exc}")

    if environment_id:
        try:
            client.delete_environment(environment_id, timeout=30.0)
        except Exception as exc:
            print(f"warning: failed to delete environment {environment_id}: {exc}")


if __name__ == "__main__":
    main()
