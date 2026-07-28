from __future__ import annotations

import os
import time
import urllib.request
from dataclasses import dataclass

from axern_sdk import AxernClient, CatalogClient, HTTPProbe, Sandbox, ServiceProbe


@dataclass(frozen=True)
class SmokeConfig:
    endpoint: str
    tls_ca_cert: str
    tls_cert: str
    tls_key: str
    service_url: str
    namespace: str
    template_id: str
    runtime_class: str
    request_cpu: str
    request_memory: str
    limit_cpu: str
    limit_memory: str


def main() -> None:
    disable_proxy_env()
    config = config_from_env()

    smoke_catalog(config)
    smoke_sandbox(config)
    smoke_service_gateway(config)


def smoke_catalog(config: SmokeConfig) -> None:
    client = CatalogClient(
        config.endpoint,
        tls_ca_cert=config.tls_ca_cert,
        tls_cert=config.tls_cert,
        tls_key=config.tls_key,
    )
    try:
        templates = client.list_runtime_templates()
        template_ids = ", ".join(template.id for template in templates[:5])
        print(f"catalog templates={len(templates)} first=[{template_ids}]")
    finally:
        client.close()


def smoke_sandbox(config: SmokeConfig) -> None:
    client = control_client(config)
    try:
        with Sandbox(
            client=client,
            namespace=config.namespace,
            template_id=config.template_id,
            runtime_class=config.runtime_class,
            request_cpu=config.request_cpu,
            request_memory=config.request_memory,
            limit_cpu=config.limit_cpu,
            limit_memory=config.limit_memory,
            labels={"axern.smoke": "gateway-python"},
        ) as sandbox:
            result = sandbox.exec("python -c \"print('sandbox ok')\"", text=True, check=True)
            print(result.stdout, end="")
            sandbox.write_text("/tmp/axern-sdk-smoke.txt", "file ok\n")
            print(sandbox.read_text("/tmp/axern-sdk-smoke.txt"), end="")
            print(f"sandbox allocation={sandbox.metadata.allocation_id} node={sandbox.metadata.node_id}")
    finally:
        client.close()


def smoke_service_gateway(config: SmokeConfig) -> None:
    client = control_client(config)
    environment_id = ""
    service_id = ""
    try:
        environment = client.create_environment(
            namespace=config.namespace,
            template_id=config.template_id,
            labels={"axern.smoke": "gateway-python"},
        )
        environment_id = environment.id

        service = client.create_service(
            namespace=config.namespace,
            environment_id=environment_id,
            replicas=1,
            argv=["python", "-m", "http.server", "8080", "--bind", "0.0.0.0"],
            runtime_class=config.runtime_class,
            request_cpu=config.request_cpu,
            request_memory=config.request_memory,
            limit_cpu=config.limit_cpu,
            limit_memory=config.limit_memory,
            readiness_probe=ServiceProbe(
                http=HTTPProbe(port=8080, path="/"),
                period=0.1,
                timeout=1.0,
                failure_threshold=3,
            ),
            labels={"axern.smoke": "gateway-python"},
        )
        service_id = service.id

        replica = wait_ready_replica(client, service_id)
        url = f"{config.service_url.rstrip('/')}/svc/{config.namespace}/{service_id}/8080/"
        status, body = fetch_without_proxy(url)
        print(f"gateway http status={status} first_line={body.splitlines()[0]}")
        print(f"service={service_id} allocation={replica.id} node={replica.node_id}")
    finally:
        cleanup(client, service_id, environment_id)
        client.close()


def control_client(config: SmokeConfig) -> AxernClient:
    return AxernClient(
        config.endpoint,
        tls_ca_cert=config.tls_ca_cert,
        tls_cert=config.tls_cert,
        tls_key=config.tls_key,
    )


def wait_ready_replica(client: AxernClient, service_id: str, timeout_seconds: float = 180.0):
    deadline = time.monotonic() + timeout_seconds
    last_replicas = []
    while time.monotonic() < deadline:
        last_replicas = client.list_service_replicas(service_id, timeout=10.0)
        for replica in last_replicas:
            if replica.ready:
                return replica
        time.sleep(2)
    raise TimeoutError(f"service {service_id} did not become ready; replicas={last_replicas!r}")


def fetch_without_proxy(url: str) -> tuple[int, str]:
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(url, timeout=20) as response:
        return response.status, response.read().decode("utf-8", errors="replace")


def cleanup(client: AxernClient, service_id: str, environment_id: str) -> None:
    if service_id:
        try:
            client.delete_service(service_id, timeout=30.0)
        except Exception as exc:
            print(f"cleanup warning: delete service {service_id}: {exc}")

    if environment_id:
        try:
            client.delete_environment(environment_id, timeout=30.0)
            print(f"cleanup environment={environment_id}")
        except Exception as exc:
            print(f"cleanup warning: delete environment {environment_id}: {exc}")


def config_from_env() -> SmokeConfig:
    return SmokeConfig(
        endpoint=required_env("AXERN_ENDPOINT"),
        tls_ca_cert=required_env("AXERN_TLS_CA_CERT"),
        tls_cert=required_env("AXERN_TLS_CERT"),
        tls_key=required_env("AXERN_TLS_KEY"),
        service_url=required_env("AXERN_SERVICE_URL"),
        namespace=os.environ.get("AXERN_EXAMPLE_NAMESPACE", "default"),
        template_id=os.environ.get("AXERN_EXAMPLE_TEMPLATE_ID", "python311"),
        runtime_class=os.environ.get("AXERN_EXAMPLE_RUNTIME_CLASS", "runsc"),
        request_cpu=os.environ.get("AXERN_EXAMPLE_REQUEST_CPU", "100m"),
        request_memory=os.environ.get("AXERN_EXAMPLE_REQUEST_MEMORY", "128Mi"),
        limit_cpu=os.environ.get("AXERN_EXAMPLE_LIMIT_CPU", "500m"),
        limit_memory=os.environ.get("AXERN_EXAMPLE_LIMIT_MEMORY", "512Mi"),
    )


def required_env(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise SystemExit(f"missing required env: {name}")
    return value


def disable_proxy_env() -> None:
    for name in (
        "HTTP_PROXY",
        "HTTPS_PROXY",
        "ALL_PROXY",
        "NO_PROXY",
        "http_proxy",
        "https_proxy",
        "all_proxy",
        "no_proxy",
    ):
        os.environ.pop(name, None)


if __name__ == "__main__":
    main()
