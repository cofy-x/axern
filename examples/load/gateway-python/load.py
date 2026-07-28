from __future__ import annotations

import concurrent.futures
from contextlib import closing
import http.client
import json
import os
import statistics
import sys
import time
import urllib.request
import urllib.parse
from dataclasses import asdict, dataclass
from typing import Any

import grpc

from axern.control.common.v1 import common_pb2
from axern.control.service.v1 import service_types_pb2
from axern_sdk import AxernClient, HTTPProbe, Sandbox, ServiceProbe


@dataclass(frozen=True)
class LoadConfig:
    endpoint: str
    tls_ca_cert: str
    tls_cert: str
    tls_key: str
    service_url: str
    namespace: str
    template_id: str
    image_ref: str
    registry_credential_id: str
    service_argv: tuple[str, ...]
    runtime_class: str
    request_cpu: str
    request_memory: str
    limit_cpu: str
    limit_memory: str
    stages: tuple[int, ...]
    phases: tuple[str, ...]
    ready_timeout_seconds: float
    http_requests_per_replica: int
    http_warmup_requests_per_worker: int
    http_profiles: tuple[str, ...]
    stage_pause_seconds: float
    prometheus_url: str
    metrics_timeout_seconds: float
    metrics_poll_interval_seconds: float


@dataclass(frozen=True)
class OperationResult:
    ok: bool
    phase: str
    stage: int
    index: int
    elapsed_seconds: float
    allocation_id: str = ""
    node_id: str = ""
    response_bytes: int = 0
    first_byte_seconds: float = 0.0
    identity: str = ""
    error: str = ""


HTTP_PROFILES = ("keepalive", "short", "large", "stream", "lb")


def main() -> None:
    disable_proxy_env()
    config = config_from_env()
    emit("config", sanitize_config(config))

    failures = 0
    for stage in config.stages:
        for phase in config.phases:
            if phase == "sandbox":
                failures += run_sandbox_stage(config, stage)
            elif phase == "service":
                failures += run_service_stage(config, stage)
            else:
                emit("failure", {"phase": phase, "stage": stage, "error": "unknown phase"})
                failures += 1
        if config.stage_pause_seconds > 0:
            time.sleep(config.stage_pause_seconds)

    if failures:
        raise SystemExit(1)


def run_sandbox_stage(config: LoadConfig, stage: int) -> int:
    started = time.monotonic()
    results: list[OperationResult] = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=stage) as pool:
        futures = [pool.submit(run_one_sandbox, config, stage, index) for index in range(stage)]
        for future in concurrent.futures.as_completed(futures):
            result = future.result()
            results.append(result)
            if not result.ok:
                emit("failure", asdict(result))

    summary = summarize_results("sandbox", stage, started, results)
    emit("summary", summary)
    return summary["failed"]


def run_one_sandbox(config: LoadConfig, stage: int, index: int) -> OperationResult:
    started = time.monotonic()
    client = control_client(config)
    try:
        with Sandbox(
            client=client,
            namespace=config.namespace,
            template_id=config.template_id,
            image=config.image_ref,
            registry_credential_id=config.registry_credential_id,
            runtime_class=config.runtime_class,
            request_cpu=config.request_cpu,
            request_memory=config.request_memory,
            limit_cpu=config.limit_cpu,
            limit_memory=config.limit_memory,
            ready_timeout_seconds=config.ready_timeout_seconds,
            labels={
                "axern.load": "gateway-python",
                "axern.load.phase": "sandbox",
                "axern.load.stage": str(stage),
            },
        ) as sandbox:
            result = sandbox.exec(
                f"printf 'load sandbox {stage}/{index} ok\\n'",
                text=True,
                check=True,
            )
            expected = f"load sandbox {stage}/{index} ok"
            stdout = (
                result.stdout.decode("utf-8", errors="replace")
                if isinstance(result.stdout, bytes)
                else result.stdout
            )
            if expected not in stdout:
                raise RuntimeError(f"unexpected stdout: {result.stdout!r}")
            path = f"/tmp/axern-load-{stage}-{index}.txt"
            payload = f"file {stage}/{index} ok\n"
            sandbox.write_text(path, payload)
            if sandbox.read_text(path) != payload:
                raise RuntimeError("file round trip mismatch")
            elapsed = time.monotonic() - started
            return OperationResult(
                ok=True,
                phase="sandbox",
                stage=stage,
                index=index,
                elapsed_seconds=elapsed,
                allocation_id=sandbox.metadata.allocation_id,
                node_id=sandbox.metadata.node_id,
            )
    except Exception as exc:
        return OperationResult(
            ok=False,
            phase="sandbox",
            stage=stage,
            index=index,
            elapsed_seconds=time.monotonic() - started,
            error=f"{type(exc).__name__}: {exc}",
        )
    finally:
        client.close()


def run_service_stage(config: LoadConfig, stage: int) -> int:
    started = time.monotonic()
    client = control_client(config)
    environment_id = ""
    service_id = ""
    failures = 0
    try:
        environment = client.create_environment(
            namespace=config.namespace,
            template_id=config.template_id,
            image_ref=config.image_ref,
            registry_credential_id=config.registry_credential_id,
            labels={
                "axern.load": "gateway-python",
                "axern.load.phase": "service",
                "axern.load.stage": str(stage),
            },
            timeout=30.0,
        )
        environment_id = environment.id
        service = client.create_service(
            namespace=config.namespace,
            environment_id=environment_id,
            replicas=stage,
            argv=list(config.service_argv),
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
            labels={
                "axern.load": "gateway-python",
                "axern.load.phase": "service",
                "axern.load.stage": str(stage),
            },
            timeout=120.0,
        )
        service_id = service.id
        replicas = wait_ready_replicas(client, service_id, stage, config.ready_timeout_seconds)
        ready_elapsed = time.monotonic() - started
        emit("service_ready", {
            "phase": "service",
            "stage": stage,
            "service_id": service_id,
            "replicas": len(replicas),
            "nodes": sorted({replica.node_id for replica in replicas}),
            "elapsed_seconds": round(ready_elapsed, 3),
        })

        request_count = max(stage * config.http_requests_per_replica, 1)
        for profile in config.http_profiles:
            metrics_before = capture_metrics(config.prometheus_url)
            results = run_gateway_http_requests(config, service_id, stage, request_count, profile)
            phase = f"service_http_{profile}"
            summary = summarize_results(phase, stage, started, results)
            summary["service_id"] = service_id
            summary["replicas"] = len(replicas)
            if profile == "lb":
                identities = sorted({result.identity for result in results if result.ok and result.identity})
                summary["identities"] = identities
                if len(identities) != stage:
                    summary["failed"] += 1
                    emit("failure", {
                        "phase": phase,
                        "stage": stage,
                        "error": f"observed {len(identities)} replica identities, expected {stage}",
                    })
            emit("summary", summary)
            workers = min(request_count, max(stage, 1))
            warmups = workers * max(config.http_warmup_requests_per_worker, 0)
            expected_metric_requests = len(results) + warmups
            metrics_summary, metrics_complete, metrics_requirements = capture_metrics_after(
                config,
                metrics_before,
                expected_metric_requests,
            )
            if config.prometheus_url:
                emit("metrics_summary", {
                    "phase": phase,
                    "stage": stage,
                    "service_id": service_id,
                    "measured_requests": len(results),
                    "warmup_requests": warmups,
                    "metrics_include_warmup": config.http_warmup_requests_per_worker > 0,
                    "complete": metrics_complete,
                    "requirements": metrics_requirements,
                    "metrics": metrics_summary,
                })
            failures += summary["failed"] + (0 if metrics_complete else 1)
    except Exception as exc:
        emit("failure", {
            "phase": "service",
            "stage": stage,
            "service_id": service_id,
            "environment_id": environment_id,
            "elapsed_seconds": round(time.monotonic() - started, 3),
            "error": f"{type(exc).__name__}: {exc}",
        })
        failures = 1
    finally:
        failures += cleanup_service(client, service_id, environment_id)
        client.close()
    return failures


def run_gateway_http_requests(
    config: LoadConfig,
    service_id: str,
    stage: int,
    request_count: int,
    profile: str,
) -> list[OperationResult]:
    workers = min(request_count, max(stage, 1))
    path, expected_bytes = profile_request(profile)
    url = f"{config.service_url.rstrip('/')}/svc/{config.namespace}/{service_id}/8080{path}"
    results: list[OperationResult] = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
        futures = [
            pool.submit(
                fetch_gateway_worker,
                url,
                stage,
                worker,
                indexes,
                config.http_warmup_requests_per_worker,
                profile,
                expected_bytes,
            )
            for worker, indexes in enumerate(distribute_indexes(request_count, workers))
        ]
        for future in concurrent.futures.as_completed(futures):
            worker_results = future.result()
            results.extend(worker_results)
            for result in worker_results:
                if not result.ok:
                    emit("failure", asdict(result))
    return results


def distribute_indexes(total: int, workers: int) -> list[list[int]]:
    indexes: list[list[int]] = [[] for _ in range(workers)]
    for index in range(total):
        indexes[index % workers].append(index)
    return indexes


def fetch_gateway_worker(
    url: str,
    stage: int,
    worker: int,
    indexes: list[int],
    warmup_requests: int,
    profile: str,
    expected_bytes: int | None,
) -> list[OperationResult]:
    parsed = urllib.parse.urlsplit(url)
    target = parsed.netloc
    path = urllib.parse.urlunsplit(("", "", parsed.path or "/", parsed.query, ""))
    keepalive = profile != "short"
    conn = http.client.HTTPConnection(target, timeout=20.0) if keepalive else None
    try:
        for warmup in range(max(warmup_requests, 0)):
            result = fetch_gateway_request(conn, target, path, stage, -1-worker-warmup, profile, expected_bytes)
            if not result.ok:
                return [result]
        return [fetch_gateway_request(conn, target, path, stage, index, profile, expected_bytes) for index in indexes]
    finally:
        if conn is not None:
            conn.close()


def fetch_gateway_request(
    connection: http.client.HTTPConnection | None,
    target: str,
    path: str,
    stage: int,
    index: int,
    profile: str,
    expected_bytes: int | None,
) -> OperationResult:
    started = time.monotonic()
    conn = connection or http.client.HTTPConnection(target, timeout=20.0)
    try:
        conn.request("GET", path)
        response = conn.getresponse()
        first = response.read(1)
        first_byte_seconds = time.monotonic() - started
        body = first + response.read()
        if response.status != 200:
            raise RuntimeError(f"status {response.status}")
        if expected_bytes is not None and len(body) != expected_bytes:
            raise RuntimeError(f"response bytes {len(body)}, expected {expected_bytes}")
        identity = body.decode("utf-8", errors="replace").strip() if profile == "lb" else ""
        validate_profile_body(profile, body)
        return OperationResult(
            ok=True,
            phase=f"service_http_{profile}",
            stage=stage,
            index=index,
            elapsed_seconds=time.monotonic() - started,
            response_bytes=len(body),
            first_byte_seconds=first_byte_seconds,
            identity=identity,
        )
    except Exception as exc:
        return OperationResult(
            ok=False,
            phase=f"service_http_{profile}",
            stage=stage,
            index=index,
            elapsed_seconds=time.monotonic() - started,
            error=f"{type(exc).__name__}: {exc}",
        )
    finally:
        if connection is None:
            conn.close()


def profile_request(profile: str) -> tuple[str, int | None]:
    if profile in {"keepalive", "short"}:
        return "/", None
    if profile == "large":
        return "/bytes?size=4194304", 4 << 20
    if profile == "stream":
        return "/stream?chunks=64&size=16384&delay_ms=2", 1 << 20
    if profile == "lb":
        return "/identity", None
    raise ValueError(f"unknown HTTP profile: {profile}")


def validate_profile_body(profile: str, body: bytes) -> None:
    if profile in {"keepalive", "short"} and not body:
        raise RuntimeError("empty response body")


def wait_ready_replicas(
    client: AxernClient,
    service_id: str,
    expected: int,
    timeout_seconds: float,
):
    deadline = time.monotonic() + timeout_seconds
    last_replicas = []
    try:
        with closing(client.watch_service(service_id, timeout=timeout_seconds)) as watch:
            for service in watch:
                remaining = max(0.1, min(10.0, deadline - time.monotonic()))
                last_replicas = client.list_service_replicas(service_id, timeout=remaining)
                ready = [
                    replica
                    for replica in last_replicas
                    if replica.ready
                    and not replica.ended
                    and not replica.outdated
                    and replica.status == common_pb2.ALLOCATION_STATUS_RUNNING
                ]
                if len(ready) >= expected:
                    return ready
                if service.status in {
                    service_types_pb2.SERVICE_STATUS_FAILED,
                    service_types_pb2.SERVICE_STATUS_DELETING,
                    service_types_pb2.SERVICE_STATUS_DELETED,
                }:
                    raise RuntimeError(
                        f"service {service_id} became {service_types_pb2.ServiceStatus.Name(service.status)}: "
                        f"{service.message}"
                    )
    except TimeoutError:
        pass
    except grpc.RpcError as exc:
        if exc.code() != grpc.StatusCode.DEADLINE_EXCEEDED or time.monotonic() < deadline:
            raise
    details = [
        {
            "id": replica.id,
            "ready": replica.ready,
            "status": common_pb2.AllocationStatus.Name(replica.status),
            "node_id": replica.node_id,
            "message": replica.message,
        }
        for replica in last_replicas
    ]
    raise TimeoutError(f"service {service_id} ready replicas < {expected}: {details}")


def cleanup_service(client: AxernClient, service_id: str, environment_id: str) -> int:
    failures = 0
    if service_id:
        try:
            client.delete_service(service_id, timeout=30.0)
        except Exception as exc:
            emit("cleanup_warning", {"service_id": service_id, "error": f"{type(exc).__name__}: {exc}"})
            failures += 1
        else:
            deadline = time.monotonic() + 180.0
            while time.monotonic() < deadline:
                try:
                    client.admin_purge_service(
                        service_id,
                        operator_reason="gateway load benchmark cleanup",
                        timeout=15.0,
                    )
                    emit("cleanup", {"service_id": service_id})
                    break
                except grpc.RpcError as exc:
                    if exc.code() != grpc.StatusCode.FAILED_PRECONDITION:
                        emit("cleanup_warning", {"service_id": service_id, "error": f"{type(exc).__name__}: {exc}"})
                        failures += 1
                        break
                except Exception as exc:
                    emit("cleanup_warning", {"service_id": service_id, "error": f"{type(exc).__name__}: {exc}"})
                    failures += 1
                    break
                time.sleep(min(2.0, max(0.0, deadline - time.monotonic())))
            else:
                emit("cleanup_warning", {"service_id": service_id, "error": "purge timed out"})
                failures += 1
    if environment_id:
        try:
            client.delete_environment(environment_id, timeout=30.0)
            emit("cleanup", {"environment_id": environment_id})
        except Exception as exc:
            emit("cleanup_warning", {"environment_id": environment_id, "error": f"{type(exc).__name__}: {exc}"})
            failures += 1
    return failures


def summarize_results(
    phase: str,
    stage: int,
    started: float,
    results: list[OperationResult],
) -> dict[str, Any]:
    latencies = [result.elapsed_seconds for result in results if result.ok]
    first_bytes = [result.first_byte_seconds for result in results if result.ok and result.first_byte_seconds > 0]
    nodes = sorted({result.node_id for result in results if result.node_id})
    return {
        "phase": phase,
        "stage": stage,
        "ok": sum(1 for result in results if result.ok),
        "failed": sum(1 for result in results if not result.ok),
        "elapsed_seconds": round(time.monotonic() - started, 3),
        "latency_seconds": latency_summary(latencies),
        "first_byte_seconds": latency_summary(first_bytes),
        "response_bytes": sum(result.response_bytes for result in results if result.ok),
        "nodes": nodes,
    }


def latency_summary(values: list[float]) -> dict[str, float]:
    if not values:
        return {}
    ordered = sorted(values)
    return {
        "min": round(ordered[0], 3),
        "p50": round(statistics.median(ordered), 3),
        "p95": round(percentile(ordered, 0.95), 3),
        "p99": round(percentile(ordered, 0.99), 3),
        "max": round(ordered[-1], 3),
    }


def percentile(ordered: list[float], q: float) -> float:
    if len(ordered) == 1:
        return ordered[0]
    pos = (len(ordered) - 1) * q
    lower = int(pos)
    upper = min(lower + 1, len(ordered) - 1)
    weight = pos - lower
    return ordered[lower] * (1 - weight) + ordered[upper] * weight


def control_client(config: LoadConfig) -> AxernClient:
    return AxernClient(
        config.endpoint,
        tls_ca_cert=config.tls_ca_cert,
        tls_cert=config.tls_cert,
        tls_key=config.tls_key,
    )


def config_from_env() -> LoadConfig:
    image_ref = os.environ.get("AXERN_LOAD_IMAGE_REF", "").strip()
    template_id = os.environ.get("AXERN_LOAD_TEMPLATE_ID", "python311").strip()
    if image_ref:
        template_id = ""
    service_argv = parse_required_str_list(
        os.environ.get(
            "AXERN_LOAD_SERVICE_ARGV",
            "/usr/local/bin/axern-startup-server"
            if image_ref
            else "python,-m,http.server,8080,--bind,0.0.0.0",
        ),
        "AXERN_LOAD_SERVICE_ARGV",
    )
    http_profiles = parse_required_str_list(
        os.environ.get("AXERN_LOAD_HTTP_PROFILES", "keepalive"),
        "AXERN_LOAD_HTTP_PROFILES",
    )
    unknown_profiles = sorted(set(http_profiles) - set(HTTP_PROFILES))
    if unknown_profiles:
        raise SystemExit(f"unknown AXERN_LOAD_HTTP_PROFILES: {', '.join(unknown_profiles)}")
    return LoadConfig(
        endpoint=required_env("AXERN_ENDPOINT"),
        tls_ca_cert=required_env("AXERN_TLS_CA_CERT"),
        tls_cert=required_env("AXERN_TLS_CERT"),
        tls_key=required_env("AXERN_TLS_KEY"),
        service_url=required_env("AXERN_SERVICE_URL"),
        namespace=os.environ.get("AXERN_LOAD_NAMESPACE", "default"),
        template_id=template_id,
        image_ref=image_ref,
        registry_credential_id=os.environ.get("AXERN_LOAD_REGISTRY_CREDENTIAL_ID", "").strip(),
        service_argv=service_argv,
        runtime_class=os.environ.get("AXERN_LOAD_RUNTIME_CLASS", "runsc"),
        request_cpu=os.environ.get("AXERN_LOAD_REQUEST_CPU", "100m"),
        request_memory=os.environ.get("AXERN_LOAD_REQUEST_MEMORY", "128Mi"),
        limit_cpu=os.environ.get("AXERN_LOAD_LIMIT_CPU", "500m"),
        limit_memory=os.environ.get("AXERN_LOAD_LIMIT_MEMORY", "512Mi"),
        stages=parse_int_list(os.environ.get("AXERN_LOAD_STAGES", "6,12,24,36")),
        phases=parse_required_str_list(os.environ.get("AXERN_LOAD_PHASES", "sandbox,service"), "AXERN_LOAD_PHASES"),
        ready_timeout_seconds=float(os.environ.get("AXERN_LOAD_READY_TIMEOUT_SECONDS", "240")),
        http_requests_per_replica=int(os.environ.get("AXERN_LOAD_HTTP_REQUESTS_PER_REPLICA", "2")),
        http_warmup_requests_per_worker=int(os.environ.get("AXERN_LOAD_HTTP_WARMUP_REQUESTS_PER_WORKER", "1")),
        http_profiles=http_profiles,
        stage_pause_seconds=float(os.environ.get("AXERN_LOAD_STAGE_PAUSE_SECONDS", "5")),
        prometheus_url=os.environ.get("AXERN_LOAD_PROMETHEUS_URL", "").strip().rstrip("/"),
        metrics_timeout_seconds=float(os.environ.get("AXERN_LOAD_METRICS_TIMEOUT_SECONDS", "45")),
        metrics_poll_interval_seconds=float(os.environ.get("AXERN_LOAD_METRICS_POLL_INTERVAL_SECONDS", "1")),
    )


def parse_int_list(value: str) -> tuple[int, ...]:
    values = tuple(int(part.strip()) for part in value.split(",") if part.strip())
    if not values or any(item <= 0 for item in values):
        raise SystemExit("AXERN_LOAD_STAGES must contain positive integers")
    return values


def parse_required_str_list(value: str, name: str) -> tuple[str, ...]:
    values = tuple(part.strip() for part in value.split(",") if part.strip())
    if not values:
        raise SystemExit(f"{name} must not be empty")
    return values


def required_env(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise SystemExit(f"missing required env: {name}")
    return value


def sanitize_config(config: LoadConfig) -> dict[str, Any]:
    data = asdict(config)
    for key in ("tls_ca_cert", "tls_cert", "tls_key"):
        data[key] = "<set>" if data[key] else "<empty>"
    data["registry_credential_id"] = "<set>" if data["registry_credential_id"] else "<empty>"
    data["prometheus_url"] = "<set>" if data["prometheus_url"] else "<empty>"
    return data


STAGE_METRICS = {
    "axern_gateway_service_proxy_stage_duration_seconds_bucket": (
        "axern_stage",
        "axern_result",
        "axern_error_class",
        "http_request_method",
    ),
    "axern_axnoded_http_proxy_stage_duration_seconds_bucket": (
        "axern_stage",
        "axern_result",
        "axern_error_class",
    ),
    "axern_axnoded_execution_lease_visibility_duration_seconds_bucket": (
        "axern_result",
    ),
}
REQUIRED_HTTP_STAGE_METRICS = (
    "axern_gateway_service_proxy_stage_duration_seconds_bucket",
    "axern_axnoded_http_proxy_stage_duration_seconds_bucket",
)
METRIC_SERIES_LABELS = ("exported_instance", "axern_node_id")


def capture_metrics(
    prometheus_url: str,
) -> dict[str, dict[tuple[str, str, str, str, str, str], dict[float, float]]]:
    snapshot: dict[str, dict[tuple[str, str, str, str, str, str], dict[float, float]]] = {}
    if not prometheus_url:
        return snapshot
    for metric, groups in STAGE_METRICS.items():
        group_by = ", ".join(("le", *groups, *METRIC_SERIES_LABELS))
        samples = query_prometheus(prometheus_url, f"sum({metric}) by ({group_by})")
        for sample in samples:
            labels = {str(key): str(value) for key, value in sample.get("labels", {}).items()}
            le = bucket_bound(labels.get("le", ""))
            key = (
                labels.get("axern_stage", ""),
                labels.get("axern_result", ""),
                labels.get("axern_error_class", ""),
                labels.get("http_request_method", ""),
                labels.get("exported_instance", ""),
                labels.get("axern_node_id", ""),
            )
            snapshot.setdefault(metric, {}).setdefault(key, {})[le] = float(sample.get("value", 0.0))
    return snapshot


def capture_metrics_after(
    config: LoadConfig,
    before: dict[str, dict[tuple[str, str, str, str, str, str], dict[float, float]]],
    expected_requests: int,
) -> tuple[list[dict[str, Any]], bool, dict[str, dict[str, int | bool]]]:
    if not config.prometheus_url:
        return [], True, {}
    deadline = time.monotonic() + config.metrics_timeout_seconds
    summaries: list[dict[str, Any]] = []
    requirements: dict[str, dict[str, int | bool]] = {}
    while True:
        try:
            after = capture_metrics(config.prometheus_url)
            summaries = summarize_metrics_delta(before, after)
            requirements = metrics_requirements(summaries, expected_requests)
            if requirements and all(bool(item["complete"]) for item in requirements.values()):
                return summaries, True, requirements
        except Exception as exc:
            emit("metrics_warning", {"error": f"{type(exc).__name__}: {exc}"})
        if time.monotonic() >= deadline:
            return summaries, False, requirements
        time.sleep(config.metrics_poll_interval_seconds)


def metrics_requirements(
    summaries: list[dict[str, Any]],
    expected_requests: int,
) -> dict[str, dict[str, int | bool]]:
    requirements: dict[str, dict[str, int | bool]] = {}
    for metric in REQUIRED_HTTP_STAGE_METRICS:
        observed = sum(
            int(summary.get("count", 0))
            for summary in summaries
            if summary.get("metric") == metric and summary.get("stage") == "total"
        )
        requirements[metric] = {
            "expected": expected_requests,
            "observed": observed,
            "complete": observed >= expected_requests,
        }
    return requirements


def summarize_metrics_delta(
    before: dict[str, dict[tuple[str, str, str, str, str, str], dict[float, float]]],
    after: dict[str, dict[tuple[str, str, str, str, str, str], dict[float, float]]],
) -> list[dict[str, Any]]:
    summaries: list[dict[str, Any]] = []
    for metric, groups in after.items():
        aggregated: dict[tuple[str, str, str, str], dict[float, float]] = {}
        for key, after_buckets in groups.items():
            before_buckets = before.get(metric, {}).get(key, {})
            delta = {
                le: counter_delta(before_buckets.get(le, 0.0), after_buckets.get(le, 0.0))
                for le in after_buckets
            }
            aggregate = aggregated.setdefault(key[:4], {})
            for le, value in delta.items():
                aggregate[le] = aggregate.get(le, 0.0) + value
        for key, delta in aggregated.items():
            count = histogram_count(delta)
            if count <= 0:
                continue
            stage, result, error_class, method = key
            summaries.append({
                "metric": metric,
                "stage": stage,
                "result": result,
                "error_class": error_class,
                "method": method,
                "count": int(count),
                "latency_seconds": {
                    "p50": round(histogram_quantile(0.50, delta), 4),
                    "p95": round(histogram_quantile(0.95, delta), 4),
                    "p99": round(histogram_quantile(0.99, delta), 4),
                },
            })
    return sorted(summaries, key=lambda item: (item["metric"], item["stage"], item["result"], item["error_class"], item["method"]))


def counter_delta(before: float, after: float) -> float:
    if after >= before:
        return after - before
    return after


def histogram_count(buckets: dict[float, float]) -> float:
    if float("inf") in buckets:
        return buckets[float("inf")]
    return max(buckets.values(), default=0.0)


def bucket_bound(value: str) -> float:
    if value in {"+Inf", "Inf", "inf"}:
        return float("inf")
    return float(value)


def histogram_quantile(q: float, cumulative_buckets: dict[float, float]) -> float:
    cumulative_buckets = monotonic_buckets(cumulative_buckets)
    total = histogram_count(cumulative_buckets)
    if total <= 0:
        return 0.0
    target = total * q
    previous_le = 0.0
    previous_count = 0.0
    for le, count in sorted(cumulative_buckets.items()):
        if le == float("inf"):
            continue
        if count >= target:
            if count <= previous_count:
                return le
            weight = (target - previous_count) / (count - previous_count)
            return previous_le + (le - previous_le) * weight
        previous_le = le
        previous_count = count
    return previous_le


def monotonic_buckets(buckets: dict[float, float]) -> dict[float, float]:
    output: dict[float, float] = {}
    previous = 0.0
    for bound, value in sorted(buckets.items()):
        previous = max(previous, value)
        output[bound] = previous
    return output


def query_prometheus(base_url: str, query: str) -> list[dict[str, Any]]:
    url = f"{base_url}/api/v1/query?{urllib.parse.urlencode({'query': query})}"
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(url, timeout=10.0) as response:
        payload = json.loads(response.read().decode("utf-8"))
    if payload.get("status") != "success":
        raise RuntimeError(payload)
    output = []
    for item in payload.get("data", {}).get("result", []):
        value = item.get("value", [None, ""])
        output.append({
            "labels": item.get("metric", {}),
            "value": float(value[1]),
        })
    return output


def emit(event: str, payload: dict[str, Any]) -> None:
    print(json.dumps({"event": event, **payload}, sort_keys=True), flush=True)


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
    try:
        main()
    except KeyboardInterrupt:
        print("interrupted", file=sys.stderr)
        raise SystemExit(130)
