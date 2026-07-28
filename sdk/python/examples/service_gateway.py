"""Create a service with the Python SDK and reach it through the Axern gateway."""

from __future__ import annotations

from collections.abc import Callable
import os
import time
import urllib.request

from axern_sdk import AxernClient
from _context import current_context

NAMESPACE = os.environ.get("AXERN_NAMESPACE", "default")
TEMPLATE_ID = os.environ.get("AXERN_TEMPLATE_ID", "python311")
RUNTIME_CLASS = os.environ.get("AXERN_RUNTIME_CLASS", "runsc")
SERVER_PROGRAM = """\
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = b"Hello from Axern\\n"
        self.send_response(200)
        self.send_header("Content-Type", "text/plain; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, format, *args):
        pass


ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
"""


def _run(on_ready: Callable[[str], None] | None = None) -> None:
    context = current_context()
    service_url = os.environ.get("AXERN_SERVICE_URL", context.service_url)
    if not service_url:
        raise SystemExit("current Axern context does not define service_url")
    client = AxernClient.from_context(os.environ.get("AXERN_CONFIG", os.path.expanduser("~/.config/axern/config.json")), context.name)
    environment_id = ""
    service_id = ""
    completed = False
    try:
        environment = client.create_environment(
            namespace=NAMESPACE,
            template_id=TEMPLATE_ID,
            labels={"axern.example": "python-sdk-service-gateway"},
        )
        environment_id = environment.id
        print(f"environment=ready template={TEMPLATE_ID}")

        service = client.create_service(
            namespace=NAMESPACE,
            environment_id=environment_id,
            replicas=1,
            argv=["python", "-c", SERVER_PROGRAM],
            runtime_class=RUNTIME_CLASS,
            request_cpu=os.environ.get("AXERN_REQUEST_CPU", "100m"),
            request_memory=os.environ.get("AXERN_REQUEST_MEMORY", "128Mi"),
            limit_cpu=os.environ.get("AXERN_LIMIT_CPU", "500m"),
            limit_memory=os.environ.get("AXERN_LIMIT_MEMORY", "512Mi"),
            labels={"axern.example": "python-sdk-service-gateway"},
        )
        service_id = service.id
        print("service=provisioning replicas=1")

        wait_ready_replica(client, service_id)
        route = f"/svc/{NAMESPACE}/{service_id}/8080/"
        url = service_url.rstrip("/") + route
        status, body = fetch(url)
        print("service=ready replicas=1")
        print(f"gateway={status} route=/svc/{NAMESPACE}/<service>/8080/")
        print(f"body={body.strip()}")
        if on_ready is not None:
            on_ready(service_id)
        completed = True
    finally:
        cleaned = cleanup(client, service_id, environment_id)
        client.close()
    if completed and cleaned:
        print("cleanup=complete")


def main() -> None:
    _run()


def wait_ready_replica(client: AxernClient, service_id: str, timeout_seconds: float = 180.0):
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        replicas = client.list_service_replicas(service_id, timeout=10.0)
        for replica in replicas:
            if replica.ready:
                return replica
        time.sleep(2)
    raise TimeoutError(f"service {service_id} did not become ready")


def fetch(url: str) -> tuple[int, str]:
    with urllib.request.urlopen(url, timeout=20) as response:
        return response.status, response.read().decode("utf-8", errors="replace")


def cleanup(client: AxernClient, service_id: str, environment_id: str) -> bool:
    cleaned = True
    if service_id:
        try:
            client.delete_service(service_id, timeout=30.0)
            wait_service_deleted(client, service_id)
        except Exception as exc:
            cleaned = False
            print(f"warning: failed to clean service {service_id}: {exc}")

    if environment_id:
        try:
            client.delete_environment(environment_id, timeout=30.0)
        except Exception as exc:
            cleaned = False
            print(f"warning: failed to delete environment {environment_id}: {exc}")

    return cleaned


def wait_service_deleted(client: AxernClient, service_id: str, timeout_seconds: float = 30.0) -> None:
    deadline = time.monotonic() + timeout_seconds
    while time.monotonic() < deadline:
        services = client.list_services(
            namespace=NAMESPACE,
            labels={"axern.example": "python-sdk-service-gateway"},
            timeout=10.0,
        )
        if all(service.id != service_id for service in services):
            return
        time.sleep(0.5)
    raise TimeoutError(f"service {service_id} was not deleted")


if __name__ == "__main__":
    main()
