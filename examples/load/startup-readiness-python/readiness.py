from __future__ import annotations

import concurrent.futures
from contextlib import closing
import http.client
import json
import math
import os
import statistics
import threading
import time
import urllib.parse
import urllib.request
from dataclasses import asdict, dataclass
from pathlib import Path
from typing import Any

import grpc

from axern.control.common.v1 import common_pb2
from axern.control.service.v1 import service_types_pb2
from axern_sdk import AxernClient, HTTPProbe, Sandbox, ServiceProbe


_EMIT_LOCK = threading.Lock()


@dataclass(frozen=True)
class Scenario:
    name: str
    image_ref: str
    port: int
    path: str
    expected_body: str
    sandbox_argv: tuple[str, ...]
    service_argv: tuple[str, ...]


@dataclass(frozen=True)
class StartupConfig:
    endpoint: str
    tls_ca_cert: str
    tls_cert: str
    tls_key: str
    service_url: str
    scenario_file: str
    registry_credential_id: str
    namespace: str
    runtime_class: str
    request_cpu: str
    request_memory: str
    limit_cpu: str
    limit_memory: str
    node_selector: dict[str, str]
    grouped_replicas_per_service: int
    stages: tuple[int, ...]
    iterations: int
    phases: tuple[str, ...]
    prometheus_url: str
    metrics_timeout_seconds: float
    metrics_poll_interval_seconds: float
    required_counter_metrics: tuple[str, ...]
    ready_timeout_seconds: float
    stage_pause_seconds: float
    scenarios: tuple[Scenario, ...]


@dataclass(frozen=True)
class Measurement:
    ok: bool
    phase: str
    topology: str
    scenario: str
    stage: int
    iteration: int
    index: int
    elapsed_seconds: float
    allocation_id: str = ""
    node_id: str = ""
    service_id: str = ""
    error: str = ""


@dataclass(frozen=True)
class HTTPMeasurement(Measurement):
    client_total_seconds: float = 0.0
    client_connect_seconds: float = 0.0
    client_request_write_seconds: float = 0.0
    client_response_headers_seconds: float = 0.0
    client_response_body_seconds: float = 0.0
    client_error_stage: str = ""


@dataclass(frozen=True)
class ServiceAttempt:
    service_id: str
    measurements: tuple[Measurement, ...]


@dataclass(frozen=True)
class SandboxAttempt:
    measurement: Measurement
    sandbox: Sandbox | None
    client: AxernClient | None


@dataclass(frozen=True)
class MetricSpec:
    name: str
    metric: str
    groups: tuple[str, ...]
    series_groups: tuple[str, ...] = ()
    matchers: tuple[tuple[str, str], ...] = ()


@dataclass(frozen=True)
class CounterSpec:
    name: str
    metric: str
    groups: tuple[str, ...]
    series_groups: tuple[str, ...] = ()
    matchers: tuple[tuple[str, str], ...] = ()


@dataclass(frozen=True)
class GaugeSpec:
    name: str
    metric: str
    groups: tuple[str, ...]
    series_groups: tuple[str, ...] = ()
    matchers: tuple[tuple[str, str], ...] = ()


@dataclass(frozen=True)
class MetricsCapture:
    captured_at: float
    buckets: dict[str, list[dict[str, Any]]]
    counters: dict[str, list[dict[str, Any]]]
    gauges: dict[str, list[dict[str, Any]]]
    errors: dict[str, str]


METRIC_QUANTILES = (0.5, 0.95, 0.99)
SERVICE_TOPOLOGIES = ("service-fanout", "service-grouped-scale", "service-replica-scale")

METRIC_SPECS = (
    MetricSpec(
        name="controld_service_allocation_queue",
        metric="axern_controld_service_allocation_queue_duration_seconds_bucket",
        groups=("axern_path", "axern_stage", "axern_result", "axern_error_class"),
        series_groups=("exported_instance",),
    ),
    MetricSpec(
        name="controld_service_replica_stage",
        metric="axern_controld_service_replica_stage_duration_seconds_bucket",
        groups=("axern_path", "axern_stage", "axern_result", "axern_error_class"),
        series_groups=("exported_instance",),
    ),
    MetricSpec(
        name="controld_service_reconcile_stage",
        metric="axern_controld_service_reconcile_stage_duration_seconds_bucket",
        groups=("axern_stage", "axern_result", "axern_error_class"),
        series_groups=("exported_instance",),
    ),
    MetricSpec(
        name="controld_service_transaction_stage",
        metric="axern_controld_service_transaction_stage_duration_seconds_bucket",
        groups=("axern_stage", "axern_result", "axern_error_class"),
        series_groups=("exported_instance",),
    ),
    MetricSpec(
        name="controld_node_lifecycle_rpc",
        metric="axern_controld_node_lifecycle_rpc_duration_seconds_bucket",
        groups=("axern_operation", "axern_stage", "axern_result", "axern_error_class"),
        series_groups=("exported_instance",),
    ),
    MetricSpec(
        name="controld_resource_admission_stage",
        metric="axern_controld_resource_admission_stage_duration_seconds_bucket",
        groups=("axern_owner_type", "axern_stage", "axern_result", "axern_error_class"),
        series_groups=("exported_instance",),
    ),
    MetricSpec(
        name="controld_allocation_status_report_stage",
        metric="axern_controld_allocation_status_report_stage_duration_seconds_bucket",
        groups=("axern_stage", "axern_result", "axern_error_class"),
        series_groups=("exported_instance",),
    ),
    MetricSpec(
        name="controld_service_status_batch_stage",
        metric="axern_controld_service_status_batch_stage_duration_seconds_bucket",
        groups=("axern_stage", "axern_result", "axern_error_class"),
        series_groups=("exported_instance",),
    ),
    MetricSpec(
        name="controld_service_replica_ready",
        metric="axern_controld_service_replica_ready_duration_seconds_bucket",
        groups=("axern_stage", "axern_result"),
        series_groups=("exported_instance",),
    ),
    MetricSpec(
        name="controld_service_ready",
        metric="axern_controld_service_ready_duration_seconds_bucket",
        groups=("axern_stage", "axern_result"),
        series_groups=("exported_instance",),
    ),
    MetricSpec(
        name="axnoded_startup_phase",
        metric="axern_axnoded_startup_phase_duration_seconds_bucket",
        groups=("axern_phase", "axern_start_class", "axern_runtime", "axern_rootfs_type", "axern_result"),
        series_groups=("exported_instance", "axern_node_id"),
    ),
    MetricSpec(
        name="axnoded_resource_allocate_stage",
        metric="axern_axnoded_resource_allocate_stage_duration_seconds_bucket",
        groups=("axern_resource", "axern_stage", "axern_result"),
        series_groups=("exported_instance", "axern_node_id"),
    ),
    MetricSpec(
        name="axnoded_startup_step",
        metric="axern_axnoded_startup_step_duration_seconds_bucket",
        groups=("axern_phase", "axern_step", "axern_start_class", "axern_runtime", "axern_rootfs_type", "axern_result"),
        series_groups=("exported_instance", "axern_node_id"),
    ),
    MetricSpec(
        name="axnoded_lifecycle_stage",
        metric="axern_axnoded_lifecycle_stage_duration_seconds_bucket",
        groups=("axern_operation", "axern_stage", "axern_runtime", "axern_result", "axern_error_class"),
        series_groups=("exported_instance", "axern_node_id"),
    ),
    MetricSpec(
        name="axnoded_allocation_delete_stage",
        metric="axern_axnoded_allocation_delete_stage_duration_seconds_bucket",
        groups=("axern_stage", "axern_runtime", "axern_result"),
        series_groups=("exported_instance", "axern_node_id"),
    ),
    MetricSpec(
        name="axnoded_allocation_status_queue_wait",
        metric="axern_axnoded_allocation_status_queue_wait_duration_seconds_bucket",
        groups=("axern_result",),
        series_groups=("exported_instance", "axern_node_id"),
    ),
    MetricSpec(
        name="imagemgr_timed_operation_stage",
        metric="axern_imagemgr_timed_operation_stage_duration_seconds_bucket",
        groups=("axern_operation", "axern_stage", "axern_phase", "axern_result"),
        series_groups=("exported_instance",),
    ),
    MetricSpec(
        name="imagefsd_readahead",
        metric="imagefsd_cache_readahead_duration_seconds_bucket",
        groups=("result",),
        series_groups=("exported_instance", "node_id"),
    ),
    MetricSpec(
        name="imagefsd_fs_read",
        metric="imagefsd_fs_read_duration_ms_milliseconds_bucket",
        groups=("image_type", "result"),
        series_groups=("exported_instance",),
    ),
    MetricSpec(
        name="imagefsd_cache_read",
        metric="imagefsd_cache_read_duration_ms_milliseconds_bucket",
        groups=("result",),
        series_groups=("exported_instance",),
    ),
    MetricSpec(
        name="imagefsd_cache_backend_fetch",
        metric="imagefsd_cache_backend_fetch_duration_ms_milliseconds_bucket",
        groups=("node_id", "path", "result"),
        series_groups=("exported_instance", "node_id"),
    ),
    MetricSpec(
        name="imagefsd_cache_inflight_wait",
        metric="imagefsd_cache_inflight_wait_duration_ms_milliseconds_bucket",
        groups=("node_id", "result"),
        series_groups=("exported_instance", "node_id"),
    ),
    MetricSpec(
        name="imagefsd_nydus_decoded_chunk_load",
        metric="imagefsd_nydus_decoded_chunk_load_duration_ms_milliseconds_bucket",
        groups=("result",),
        series_groups=("exported_instance",),
    ),
    MetricSpec(
        name="dragonfly_seed_backend_request",
        metric="dragonfly_client_backend_request_duration_milliseconds_bucket",
        groups=("pod", "method", "scheme"),
        matchers=(("job", "dragonfly-seed-client"),),
    ),
    MetricSpec(
        name="dragonfly_seed_download_task",
        metric="dragonfly_client_download_task_duration_milliseconds_bucket",
        groups=("pod", "task_type", "task_size_level"),
        matchers=(("job", "dragonfly-seed-client"),),
    ),
    MetricSpec(
        name="axnoded_readiness_wait",
        metric="axern_axnoded_readiness_wait_duration_seconds_bucket",
        groups=("axern_probe_type", "axern_result"),
        series_groups=("exported_instance", "axern_node_id"),
    ),
    MetricSpec(
        name="axnoded_probe_attempt",
        metric="axern_axnoded_probe_attempt_duration_seconds_bucket",
        groups=("axern_probe_kind", "axern_probe_type", "axern_result"),
        series_groups=("exported_instance", "axern_node_id"),
    ),
    MetricSpec(
        name="axnoded_readiness_probe_stage",
        metric="axern_axnoded_readiness_probe_stage_duration_seconds_bucket",
        groups=("axern_probe_type", "axern_stage", "axern_result", "axern_error_class"),
        series_groups=("exported_instance", "axern_node_id"),
    ),
    MetricSpec(
        name="axnoded_control_plane_rpc",
        metric="axern_axnoded_control_plane_rpc_duration_seconds_bucket",
        groups=("axern_operation", "axern_result"),
        series_groups=("exported_instance", "axern_node_id"),
    ),
    MetricSpec(
        name="gateway_service_proxy_stage",
        metric="axern_gateway_service_proxy_stage_duration_seconds_bucket",
        groups=("axern_stage", "axern_result", "axern_error_class", "http_request_method"),
        series_groups=("exported_instance",),
    ),
    MetricSpec(
        name="axnoded_http_proxy_stage",
        metric="axern_axnoded_http_proxy_stage_duration_seconds_bucket",
        groups=("axern_stage", "axern_result", "axern_error_class"),
        series_groups=("exported_instance", "axern_node_id"),
    ),
    MetricSpec(
        name="axnoded_execution_lease_visibility",
        metric="axern_axnoded_execution_lease_visibility_duration_seconds_bucket",
        groups=("axern_result",),
        series_groups=("exported_instance", "axern_node_id"),
    ),
)

COUNTER_SPECS = (
    CounterSpec(
        name="imagefsd_nydus_decoded_chunk_access",
        metric="imagefsd_nydus_decoded_chunk_access_total",
        groups=("result",),
        series_groups=("exported_instance",),
    ),
    CounterSpec(
        name="imagefsd_nydus_decoded_chunk_load_bytes",
        metric="imagefsd_nydus_decoded_chunk_load_bytes_total",
        groups=("result",),
        series_groups=("exported_instance",),
    ),
    CounterSpec(
        name="imagefsd_nydus_decoded_chunk_evictions",
        metric="imagefsd_nydus_decoded_chunk_eviction_total",
        groups=(),
        series_groups=("exported_instance",),
    ),
    CounterSpec(
        name="imagefsd_fs_read_bytes",
        metric="imagefsd_fs_read_bytes_total",
        groups=("image_type", "result"),
        series_groups=("exported_instance",),
    ),
    CounterSpec(
        name="imagefsd_cache_read_bytes",
        metric="imagefsd_cache_read_bytes_total",
        groups=("result",),
        series_groups=("exported_instance",),
    ),
    CounterSpec(
        name="imagefsd_cache_backend_fetch_bytes",
        metric="imagefsd_cache_backend_fetch_bytes_total",
        groups=("result",),
        series_groups=("exported_instance",),
    ),
    CounterSpec(
        name="dragonfly_seed_proxy_requests",
        metric="dragonfly_client_proxy_request_total",
        groups=("pod",),
        matchers=(("job", "dragonfly-seed-client"),),
    ),
    CounterSpec(
        name="dragonfly_seed_proxy_failures",
        metric="dragonfly_client_proxy_request_failure_total",
        groups=("pod",),
        matchers=(("job", "dragonfly-seed-client"),),
    ),
    CounterSpec(
        name="dragonfly_seed_proxy_via_dfdaemon",
        metric="dragonfly_client_proxy_request_via_dfdaemon_total",
        groups=("pod",),
        matchers=(("job", "dragonfly-seed-client"),),
    ),
    CounterSpec(
        name="dragonfly_seed_backend_requests",
        metric="dragonfly_client_backend_request_total",
        groups=("pod", "method", "scheme"),
        matchers=(("job", "dragonfly-seed-client"),),
    ),
    CounterSpec(
        name="dragonfly_seed_backend_failures",
        metric="dragonfly_client_backend_request_failure_total",
        groups=("pod", "method", "scheme"),
        matchers=(("job", "dragonfly-seed-client"),),
    ),
    CounterSpec(
        name="dragonfly_seed_download_tasks",
        metric="dragonfly_client_download_task_total",
        groups=("pod", "type", "priority"),
        matchers=(("job", "dragonfly-seed-client"),),
    ),
    CounterSpec(
        name="dragonfly_seed_download_bytes",
        metric="dragonfly_client_download_traffic",
        groups=("pod", "type"),
        matchers=(("job", "dragonfly-seed-client"),),
    ),
)

GAUGE_SPECS = (
    GaugeSpec(
        name="imagefsd_nydus_decoded_chunk_current_bytes",
        metric="imagefsd_nydus_decoded_chunk_current_bytes",
        groups=(),
        series_groups=("exported_instance",),
    ),
)


def main() -> None:
    if truthy(os.environ.get("AXERN_STARTUP_DIRECT_GATEWAY", "true")):
        disable_proxy_env()

    config = config_from_env()
    emit("config", sanitize_config(config))

    failures = 0
    all_results: list[Measurement] = []
    for iteration in range(1, config.iterations + 1):
        for scenario in config.scenarios:
            for stage in config.stages:
                if "sandbox" in config.phases:
                    metrics_before = capture_metrics_before(config)
                    attempts = run_sandbox_stage(config, scenario, stage, iteration)
                    results = [attempt.measurement for attempt in attempts]
                    failures += emit_stage_summary("sandbox_ready", "sandbox", scenario.name, stage, iteration, results)
                    cleanup_results = cleanup_sandbox_stage(attempts, scenario.name, stage, iteration)
                    results.extend(cleanup_results)
                    failures += emit_stage_summary(
                        "sandbox_cleanup",
                        "sandbox",
                        scenario.name,
                        stage,
                        iteration,
                        cleanup_results,
                    )
                    if not emit_metrics_summary(
                        config,
                        "sandbox_stage",
                        "sandbox",
                        scenario.name,
                        stage,
                        iteration,
                        metrics_before,
                    ):
                        failures += 1
                    all_results.extend(results)
                for topology in SERVICE_TOPOLOGIES:
                    if topology not in config.phases:
                        continue
                    metrics_before = capture_metrics_before(config)
                    results, metrics_complete = run_service_stage(
                        config,
                        scenario,
                        topology,
                        stage,
                        iteration,
                        metrics_before,
                    )
                    all_results.extend(results)
                    for phase in (
                        "service_create_ack",
                        "service_replica_ready",
                        "service_ready",
                        "service_first_http",
                        "service_workload",
                        "service_cleanup",
                    ):
                        failures += emit_stage_summary(
                            phase,
                            topology,
                            scenario.name,
                            stage,
                            iteration,
                            results,
                        )
                    if not metrics_complete:
                        failures += 1
                if config.stage_pause_seconds > 0:
                    time.sleep(config.stage_pause_seconds)

    emit_final_summaries(all_results)
    if failures:
        raise SystemExit(1)


def run_sandbox_stage(
    config: StartupConfig,
    scenario: Scenario,
    stage: int,
    iteration: int,
) -> list[SandboxAttempt]:
    attempts: list[SandboxAttempt] = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=stage) as pool:
        futures = [
            pool.submit(run_one_sandbox, config, scenario, stage, iteration, index)
            for index in range(stage)
        ]
        for future in concurrent.futures.as_completed(futures):
            attempt = future.result()
            attempts.append(attempt)
            if attempt.measurement.ok:
                emit("sandbox_ready", asdict(attempt.measurement))
            else:
                emit("failure", asdict(attempt.measurement))
    return attempts


def run_one_sandbox(
    config: StartupConfig,
    scenario: Scenario,
    stage: int,
    iteration: int,
    index: int,
) -> SandboxAttempt:
    started = time.monotonic()
    client: AxernClient | None = None
    sandbox: Sandbox | None = None
    try:
        client = control_client(config)
        sandbox = Sandbox(
            client=client,
            namespace=config.namespace,
            image=scenario.image_ref,
            registry_credential_id=config.registry_credential_id,
            argv=list(scenario.sandbox_argv),
            runtime_class=config.runtime_class,
            request_cpu=config.request_cpu,
            request_memory=config.request_memory,
            limit_cpu=config.limit_cpu,
            limit_memory=config.limit_memory,
            ready_timeout_seconds=config.ready_timeout_seconds,
            labels=labels("startup-readiness", "sandbox", scenario.name, stage, iteration),
        )
        sandbox.start()
        return SandboxAttempt(
            measurement=Measurement(
                ok=True,
                phase="sandbox_ready",
                topology="sandbox",
                scenario=scenario.name,
                stage=stage,
                iteration=iteration,
                index=index,
                elapsed_seconds=round(time.monotonic() - started, 3),
                allocation_id=sandbox.metadata.allocation_id,
                node_id=sandbox.metadata.node_id,
                service_id=sandbox.metadata.service_id,
            ),
            sandbox=sandbox,
            client=client,
        )
    except Exception as exc:
        if sandbox is not None:
            sandbox.close()
        if client is not None:
            try:
                client.close()
            except Exception:
                pass
        return SandboxAttempt(
            measurement=Measurement(
                ok=False,
                phase="sandbox_ready",
                topology="sandbox",
                scenario=scenario.name,
                stage=stage,
                iteration=iteration,
                index=index,
                elapsed_seconds=round(time.monotonic() - started, 3),
                error=f"{type(exc).__name__}: {exc}",
            ),
            sandbox=None,
            client=None,
        )


def cleanup_sandbox_stage(
    attempts: list[SandboxAttempt],
    scenario: str,
    stage: int,
    iteration: int,
) -> list[Measurement]:
    active = [attempt for attempt in attempts if attempt.sandbox is not None and attempt.client is not None]
    if not active:
        return []
    results: list[Measurement] = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=min(len(active), 16)) as pool:
        futures = [
            pool.submit(cleanup_one_sandbox, attempt, scenario, stage, iteration)
            for attempt in active
        ]
        for future in concurrent.futures.as_completed(futures):
            measurement = future.result()
            results.append(measurement)
            emit("cleanup" if measurement.ok else "failure", asdict(measurement))
    return results


def cleanup_one_sandbox(
    attempt: SandboxAttempt,
    scenario: str,
    stage: int,
    iteration: int,
) -> Measurement:
    started = time.monotonic()
    sandbox = attempt.sandbox
    client = attempt.client
    if sandbox is None or client is None:
        raise ValueError("sandbox cleanup requires an active sandbox and client")
    errors: list[str] = []
    service_id = attempt.measurement.service_id
    environment_id = sandbox.metadata.environment_id
    if service_id:
        _, error = cleanup_service(client, service_id)
        if error:
            errors.append(f"service {service_id}: {error}")
    if environment_id:
        try:
            client.delete_environment(environment_id, timeout=30.0)
            emit("cleanup", {"environment_id": environment_id})
        except Exception as exc:
            errors.append(f"environment {environment_id}: {type(exc).__name__}: {exc}")
    sandbox.close()
    try:
        client.close()
    except Exception as exc:
        errors.append(f"client: {type(exc).__name__}: {exc}")
    return Measurement(
        ok=not errors,
        phase="sandbox_cleanup",
        topology="sandbox",
        scenario=scenario,
        stage=stage,
        iteration=iteration,
        index=attempt.measurement.index,
        elapsed_seconds=round(time.monotonic() - started, 3),
        allocation_id=attempt.measurement.allocation_id,
        node_id=attempt.measurement.node_id,
        service_id=service_id,
        error="; ".join(errors),
    )


def run_service_stage(
    config: StartupConfig,
    scenario: Scenario,
    topology: str,
    stage: int,
    iteration: int,
    metrics_before: MetricsCapture | None,
) -> tuple[list[Measurement], bool]:
    client = control_client(config)
    environment_id = ""
    service_ids: list[str] = []
    results: list[Measurement] = []
    metrics_complete = False
    workload_complete = False
    try:
        environment_started = time.monotonic()
        environment = client.create_environment(
            namespace=config.namespace,
            image_ref=scenario.image_ref,
            registry_credential_id=config.registry_credential_id,
            labels=labels("startup-readiness", topology, scenario.name, stage, iteration),
            timeout=30.0,
        )
        environment_id = environment.id
        emit("environment_created", {
            "topology": topology,
            "scenario": scenario.name,
            "stage": stage,
            "iteration": iteration,
            "environment_id": environment_id,
            "elapsed_seconds": round(time.monotonic() - environment_started, 3),
        })

        attempts = run_service_attempts(
            config,
            client,
            scenario,
            topology,
            environment_id,
            stage,
            iteration,
        )
        for attempt in attempts:
            if attempt.service_id:
                service_ids.append(attempt.service_id)
            results.extend(attempt.measurements)
        workload_complete = True
    except Exception as exc:
        failure = Measurement(
            ok=False,
            phase="service_workload",
            topology=topology,
            scenario=scenario.name,
            stage=stage,
            iteration=iteration,
            index=len(results),
            elapsed_seconds=0.0,
            error=f"{type(exc).__name__}: {exc}",
        )
        emit("failure", asdict(failure))
        results.append(failure)
    finally:
        for resource_type, resource_id, error in cleanup_service_stage(client, service_ids, environment_id):
            failure = Measurement(
                ok=False,
                phase="service_cleanup",
                topology=topology,
                scenario=scenario.name,
                stage=stage,
                iteration=iteration,
                index=len(results),
                elapsed_seconds=0.0,
                service_id=resource_id if resource_type == "service" else "",
                error=error,
            )
            results.append(failure)
            emit("failure", asdict(failure) | {"resource_type": resource_type, "resource_id": resource_id})
        try:
            client.close()
        except Exception as exc:
            failure = Measurement(
                ok=False,
                phase="service_cleanup",
                topology=topology,
                scenario=scenario.name,
                stage=stage,
                iteration=iteration,
                index=len(results),
                elapsed_seconds=0.0,
                error=f"{type(exc).__name__}: {exc}",
            )
            results.append(failure)
            emit("failure", asdict(failure) | {"resource_type": "client", "resource_id": "control"})
    if workload_complete:
        metrics_complete = emit_metrics_summary(
            config,
            "service_stage",
            topology,
            scenario.name,
            stage,
            iteration,
            metrics_before,
        )
    return results, metrics_complete


def run_service_attempts(
    config: StartupConfig,
    client: AxernClient,
    scenario: Scenario,
    topology: str,
    environment_id: str,
    stage: int,
    iteration: int,
) -> list[ServiceAttempt]:
    service_count, replicas_per_service = service_topology_shape(
        topology,
        stage,
        config.grouped_replicas_per_service,
    )
    clock: dict[str, float] = {}
    barrier = threading.Barrier(
        service_count + 1,
        action=lambda: clock.update(started=time.monotonic()),
    )
    attempts: list[ServiceAttempt] = []
    with concurrent.futures.ThreadPoolExecutor(max_workers=service_count) as pool:
        futures = [
            pool.submit(
                run_one_service,
                config,
                client,
                scenario,
                topology,
                environment_id,
                stage,
                iteration,
                service_index,
                replicas_per_service,
                barrier,
                clock,
            )
            for service_index in range(service_count)
        ]
        barrier.wait()
        for future in concurrent.futures.as_completed(futures):
            attempts.append(future.result())
    return attempts


def run_one_service(
    config: StartupConfig,
    client: AxernClient,
    scenario: Scenario,
    topology: str,
    environment_id: str,
    stage: int,
    iteration: int,
    service_index: int,
    replicas: int,
    barrier: threading.Barrier,
    clock: dict[str, float],
) -> ServiceAttempt:
    service_id = ""
    results: list[Measurement] = []
    try:
        barrier.wait()
        started = clock["started"]
        service = client.create_service(
            namespace=config.namespace,
            environment_id=environment_id,
            replicas=replicas,
            argv=list(scenario.service_argv),
            runtime_class=config.runtime_class,
            request_cpu=config.request_cpu,
            request_memory=config.request_memory,
            limit_cpu=config.limit_cpu,
            limit_memory=config.limit_memory,
            node_selector=config.node_selector,
            readiness_probe=ServiceProbe(
                http=HTTPProbe(port=scenario.port, path=scenario.path),
                period=0.1,
                timeout=1.0,
                failure_threshold=3,
            ),
            labels=labels("startup-readiness", topology, scenario.name, stage, iteration),
            timeout=120.0,
        )
        service_id = service.id
        created = Measurement(
            ok=True,
            phase="service_create_ack",
            topology=topology,
            scenario=scenario.name,
            stage=stage,
            iteration=iteration,
            index=service_index,
            elapsed_seconds=round(time.monotonic() - started, 3),
            service_id=service_id,
        )
        results.append(created)
        emit("service_create_ack", asdict(created))

        ready, readiness_error = wait_ready_replicas(
            config,
            client,
            scenario,
            topology,
            service_id,
            stage,
            iteration,
            service_index,
            replicas,
            started,
        )
        results.extend(ready)
        if readiness_error:
            raise TimeoutError(readiness_error)
        service_ready = Measurement(
            ok=True,
            phase="service_ready",
            topology=topology,
            scenario=scenario.name,
            stage=stage,
            iteration=iteration,
            index=service_index,
            elapsed_seconds=round(time.monotonic() - started, 3),
            service_id=service_id,
        )
        results.append(service_ready)
        emit("service_ready", asdict(service_ready))
        emit("service_all_ready", {
            "topology": topology,
            "scenario": scenario.name,
            "stage": stage,
            "iteration": iteration,
            "index": service_index,
            "service_id": service_id,
            "replicas": len(ready),
            "nodes": sorted({result.node_id for result in ready if result.node_id}),
            "node_counts": node_counts(ready),
            "elapsed_seconds": service_ready.elapsed_seconds,
        })

        http_result = fetch_first_gateway_http(
            config,
            scenario,
            topology,
            service_id,
            stage,
            iteration,
            service_index,
            started,
        )
        results.append(http_result)
        emit("service_first_http" if http_result.ok else "failure", asdict(http_result))
    except Exception as exc:
        failure = Measurement(
            ok=False,
            phase="service_workload",
            topology=topology,
            scenario=scenario.name,
            stage=stage,
            iteration=iteration,
            index=service_index,
            elapsed_seconds=round(time.monotonic() - clock.get("started", time.monotonic()), 3),
            service_id=service_id,
            error=f"{type(exc).__name__}: {exc}",
        )
        results.append(failure)
        emit("failure", asdict(failure))
    return ServiceAttempt(service_id=service_id, measurements=tuple(results))


def service_topology_shape(topology: str, stage: int, grouped_replicas: int = 4) -> tuple[int, int]:
    if topology == "service-fanout":
        return stage, 1
    if topology == "service-grouped-scale":
        if stage % grouped_replicas != 0:
            raise ValueError(
                f"service-grouped-scale stage {stage} must be divisible by "
                f"AXERN_STARTUP_GROUPED_REPLICAS_PER_SERVICE={grouped_replicas}"
            )
        return stage // grouped_replicas, grouped_replicas
    if topology == "service-replica-scale":
        return 1, stage
    raise ValueError(f"unsupported service topology: {topology}")


def wait_ready_replicas(
    config: StartupConfig,
    client: AxernClient,
    scenario: Scenario,
    topology: str,
    service_id: str,
    stage: int,
    iteration: int,
    service_index: int,
    expected: int,
    started: float,
) -> tuple[list[Measurement], str]:
    deadline = time.monotonic() + config.ready_timeout_seconds
    ready: dict[str, Measurement] = {}
    last_replicas = []
    last_version = 0
    terminal_error = ""
    while not terminal_error:
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            break
        reconnect = True
        try:
            with closing(
                client.watch_service(
                    service_id,
                    after_version=last_version,
                    timeout=remaining,
                )
            ) as watch:
                for service in watch:
                    remaining = deadline - time.monotonic()
                    if remaining <= 0:
                        break
                    try:
                        last_replicas = client.list_service_replicas(
                            service_id,
                            timeout=min(10.0, remaining),
                        )
                    except TimeoutError:
                        reconnect = True
                        break
                    except grpc.RpcError as exc:
                        if exc.code() not in {
                            grpc.StatusCode.UNAVAILABLE,
                            grpc.StatusCode.DEADLINE_EXCEEDED,
                        }:
                            raise
                        reconnect = True
                        break
                    last_version = max(last_version, service.version)
                    for replica in last_replicas:
                        if replica.id in ready:
                            continue
                        if is_ready_replica(replica):
                            measurement = Measurement(
                                ok=True,
                                phase="service_replica_ready",
                                topology=topology,
                                scenario=scenario.name,
                                stage=stage,
                                iteration=iteration,
                                index=service_index * expected + len(ready),
                                elapsed_seconds=round(time.monotonic() - started, 3),
                                allocation_id=replica.id,
                                node_id=replica.node_id,
                                service_id=service_id,
                            )
                            ready[replica.id] = measurement
                            emit("service_replica_ready", asdict(measurement))
                    if len(ready) >= expected:
                        return list(ready.values()), ""
                    if service.status in {
                        service_types_pb2.SERVICE_STATUS_FAILED,
                        service_types_pb2.SERVICE_STATUS_DELETING,
                        service_types_pb2.SERVICE_STATUS_DELETED,
                    }:
                        terminal_error = (
                            f"service {service_id} became {service_types_pb2.ServiceStatus.Name(service.status)}: "
                            f"{service.message}"
                        )
                        break
        except TimeoutError:
            reconnect = True
        except grpc.RpcError as exc:
            if exc.code() not in {
                grpc.StatusCode.UNAVAILABLE,
                grpc.StatusCode.DEADLINE_EXCEEDED,
            }:
                raise
            reconnect = True

        remaining = deadline - time.monotonic()
        if not terminal_error and remaining > 0 and reconnect:
            time.sleep(min(0.1, remaining))

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
    if terminal_error:
        return list(ready.values()), f"{terminal_error}; replicas: {details}"
    return list(ready.values()), f"service {service_id} ready replicas < {expected}: {details}"


def is_ready_replica(replica: Any) -> bool:
    return (
        replica.ready
        and not replica.ended
        and not replica.outdated
        and replica.status == common_pb2.ALLOCATION_STATUS_RUNNING
    )


def fetch_first_gateway_http(
    config: StartupConfig,
    scenario: Scenario,
    topology: str,
    service_id: str,
    stage: int,
    iteration: int,
    service_index: int,
    started: float,
) -> HTTPMeasurement:
    target = normalize_http_target(config.service_url)
    path = gateway_service_path(config.namespace, service_id, scenario.port, scenario.path)
    conn = http.client.HTTPConnection(target, timeout=20.0)
    http_started = time.monotonic()
    stage_started = http_started
    connect_seconds = 0.0
    request_write_seconds = 0.0
    response_headers_seconds = 0.0
    response_body_seconds = 0.0
    client_stage = "connect"
    try:
        conn.connect()
        stage_completed = time.monotonic()
        connect_seconds = stage_completed - stage_started
        stage_started = stage_completed
        client_stage = "request_write"
        conn.request("GET", path)
        stage_completed = time.monotonic()
        request_write_seconds = stage_completed - stage_started
        stage_started = stage_completed
        client_stage = "response_headers"
        response = conn.getresponse()
        stage_completed = time.monotonic()
        response_headers_seconds = stage_completed - stage_started
        stage_started = stage_completed
        client_stage = "response_body"
        body = response.read().decode("utf-8", errors="replace")
        completed = time.monotonic()
        response_body_seconds = completed - stage_started
        client_stage = "validate_response"
        if response.status != 200:
            raise RuntimeError(f"status {response.status}")
        if scenario.expected_body and scenario.expected_body not in body:
            raise RuntimeError(f"unexpected body prefix: {body[:120]!r}")
        return HTTPMeasurement(
            ok=True,
            phase="service_first_http",
            topology=topology,
            scenario=scenario.name,
            stage=stage,
            iteration=iteration,
            index=service_index,
            elapsed_seconds=round(time.monotonic() - started, 3),
            service_id=service_id,
            client_total_seconds=round(completed - http_started, 6),
            client_connect_seconds=round(connect_seconds, 6),
            client_request_write_seconds=round(request_write_seconds, 6),
            client_response_headers_seconds=round(response_headers_seconds, 6),
            client_response_body_seconds=round(response_body_seconds, 6),
        )
    except Exception as exc:
        completed = time.monotonic()
        unfinished_seconds = completed - stage_started
        if client_stage == "connect":
            connect_seconds = unfinished_seconds
        elif client_stage == "request_write":
            request_write_seconds = unfinished_seconds
        elif client_stage == "response_headers":
            response_headers_seconds = unfinished_seconds
        elif client_stage == "response_body":
            response_body_seconds = unfinished_seconds
        return HTTPMeasurement(
            ok=False,
            phase="service_first_http",
            topology=topology,
            scenario=scenario.name,
            stage=stage,
            iteration=iteration,
            index=service_index,
            elapsed_seconds=round(time.monotonic() - started, 3),
            service_id=service_id,
            error=f"{type(exc).__name__}: {exc}",
            client_total_seconds=round(completed - http_started, 6),
            client_connect_seconds=round(connect_seconds, 6),
            client_request_write_seconds=round(request_write_seconds, 6),
            client_response_headers_seconds=round(response_headers_seconds, 6),
            client_response_body_seconds=round(response_body_seconds, 6),
            client_error_stage=client_stage,
        )
    finally:
        conn.close()


def gateway_service_path(namespace: str, service_id: str, port: int, path: str) -> str:
    clean_path = path if path.startswith("/") else f"/{path}"
    return f"/svc/{namespace}/{service_id}/{port}{clean_path}"


def normalize_http_target(target: str) -> str:
    if "://" not in target:
        return target
    parsed = urllib.parse.urlsplit(target)
    if not parsed.netloc:
        raise SystemExit(f"invalid AXERN_SERVICE_URL: {target}")
    return parsed.netloc


def cleanup_service_stage(
    client: AxernClient,
    service_ids: list[str],
    environment_id: str,
) -> list[tuple[str, str, str]]:
    failures: list[tuple[str, str, str]] = []
    if service_ids:
        with concurrent.futures.ThreadPoolExecutor(max_workers=min(len(service_ids), 16)) as pool:
            futures = [pool.submit(cleanup_service, client, service_id) for service_id in service_ids]
            for future in concurrent.futures.as_completed(futures):
                service_id, error = future.result()
                if error:
                    failures.append(("service", service_id, error))
    if environment_id:
        try:
            client.delete_environment(environment_id, timeout=30.0)
            emit("cleanup", {"environment_id": environment_id})
        except Exception as exc:
            error = f"{type(exc).__name__}: {exc}"
            emit("cleanup_warning", {"environment_id": environment_id, "error": error})
            failures.append(("environment", environment_id, error))
    return failures


def cleanup_service(client: AxernClient, service_id: str) -> tuple[str, str]:
    started = time.monotonic()
    try:
        client.delete_service(service_id, timeout=30.0)
    except Exception as exc:
        error = f"{type(exc).__name__}: {exc}"
        emit("cleanup_warning", {"service_id": service_id, "stage": "delete", "error": error})
        return service_id, error

    deleted_at = time.monotonic()
    deadline = time.monotonic() + 180.0
    while time.monotonic() < deadline:
        try:
            client.admin_purge_service(
                service_id,
                operator_reason="startup readiness benchmark cleanup",
                timeout=15.0,
            )
            purged_at = time.monotonic()
            emit("cleanup", {
                "service_id": service_id,
                "delete_ack_seconds": round(deleted_at - started, 6),
                "delete_to_purge_seconds": round(purged_at - deleted_at, 6),
                "total_seconds": round(purged_at - started, 6),
            })
            return service_id, ""
        except grpc.RpcError as exc:
            if exc.code() != grpc.StatusCode.FAILED_PRECONDITION:
                error = f"{type(exc).__name__}: {exc}"
                emit("cleanup_warning", {"service_id": service_id, "stage": "purge", "error": error})
                return service_id, error
        except Exception as exc:
            error = f"{type(exc).__name__}: {exc}"
            emit("cleanup_warning", {"service_id": service_id, "stage": "purge", "error": error})
            return service_id, error
        time.sleep(min(2.0, max(0.0, deadline - time.monotonic())))
    error = "purge timed out"
    emit("cleanup_warning", {"service_id": service_id, "stage": "purge", "error": error})
    return service_id, error


def emit_stage_summary(
    phase: str,
    topology: str,
    scenario: str,
    stage: int,
    iteration: int,
    results: list[Measurement],
) -> int:
    phase_results = [result for result in results if result.phase == phase]
    if not phase_results:
        return 0
    failed = sum(1 for result in phase_results if not result.ok)
    emit("summary", {
        "phase": phase,
        "topology": topology,
        "scenario": scenario,
        "stage": stage,
        "iteration": iteration,
        "ok": sum(1 for result in phase_results if result.ok),
        "failed": failed,
        "latency_seconds": latency_summary([result.elapsed_seconds for result in phase_results if result.ok]),
        "nodes": sorted({result.node_id for result in phase_results if result.node_id}),
        "node_counts": node_counts(phase_results),
    })
    return failed


def emit_final_summaries(results: list[Measurement]) -> None:
    groups: dict[tuple[str, str, str, int], list[Measurement]] = {}
    for result in results:
        groups.setdefault((result.phase, result.topology, result.scenario, result.stage), []).append(result)
    for key in sorted(groups):
        phase, topology, scenario, stage = key
        values = groups[key]
        emit("summary", {
            "scope": "final",
            "phase": phase,
            "topology": topology,
            "scenario": scenario,
            "stage": stage,
            "ok": sum(1 for result in values if result.ok),
            "failed": sum(1 for result in values if not result.ok),
            "latency_seconds": latency_summary([result.elapsed_seconds for result in values if result.ok]),
            "nodes": sorted({result.node_id for result in values if result.node_id}),
            "node_counts": node_counts(values),
        })


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


def node_counts(results: list[Measurement]) -> dict[str, int]:
    counts: dict[str, int] = {}
    for result in results:
        if result.node_id:
            counts[result.node_id] = counts.get(result.node_id, 0) + 1
    return dict(sorted(counts.items()))


def percentile(ordered: list[float], q: float) -> float:
    if len(ordered) == 1:
        return ordered[0]
    pos = (len(ordered) - 1) * q
    lower = int(pos)
    upper = min(lower + 1, len(ordered) - 1)
    weight = pos - lower
    return ordered[lower] * (1 - weight) + ordered[upper] * weight


def labels(kind: str, topology: str, scenario: str, stage: int, iteration: int) -> dict[str, str]:
    return {
        "axern.load": kind,
        "axern.load.topology": topology,
        "axern.load.scenario": scenario,
        "axern.load.stage": str(stage),
        "axern.load.iteration": str(iteration),
    }


def control_client(config: StartupConfig) -> AxernClient:
    return AxernClient(
        config.endpoint,
        tls_ca_cert=config.tls_ca_cert,
        tls_cert=config.tls_cert,
        tls_key=config.tls_key,
    )


def config_from_env() -> StartupConfig:
    phases = parse_str_list(
        os.environ.get("AXERN_STARTUP_PHASES", "sandbox,service-fanout,service-replica-scale"),
        "AXERN_STARTUP_PHASES",
    )
    unknown = sorted(set(phases) - {"sandbox", *SERVICE_TOPOLOGIES})
    if unknown:
        raise SystemExit(f"unsupported AXERN_STARTUP_PHASES value(s): {', '.join(unknown)}")

    scenario_file = resolve_local_path(required_env("AXERN_STARTUP_SCENARIO_FILE"))
    selected = parse_optional_str_list(os.environ.get("AXERN_STARTUP_SCENARIOS", ""))
    scenarios = load_scenarios(scenario_file, selected)
    service_url = os.environ.get("AXERN_SERVICE_URL", "").strip()
    if any(topology in phases for topology in SERVICE_TOPOLOGIES) and not service_url:
        raise SystemExit("missing required env: AXERN_SERVICE_URL")
    required_counter_metrics = parse_optional_str_list(
        os.environ.get("AXERN_STARTUP_REQUIRED_COUNTER_METRICS", "")
    )
    known_counter_metrics = {spec.name for spec in COUNTER_SPECS}
    unknown_counter_metrics = sorted(set(required_counter_metrics) - known_counter_metrics)
    if unknown_counter_metrics:
        raise SystemExit(
            "unsupported AXERN_STARTUP_REQUIRED_COUNTER_METRICS value(s): "
            + ", ".join(unknown_counter_metrics)
        )

    return StartupConfig(
        endpoint=required_env("AXERN_ENDPOINT"),
        tls_ca_cert=resolve_local_path(required_env("AXERN_TLS_CA_CERT")),
        tls_cert=resolve_local_path(required_env("AXERN_TLS_CERT")),
        tls_key=resolve_local_path(required_env("AXERN_TLS_KEY")),
        service_url=service_url,
        scenario_file=scenario_file,
        registry_credential_id=os.environ.get("AXERN_STARTUP_REGISTRY_CREDENTIAL_ID", "").strip(),
        namespace=os.environ.get("AXERN_STARTUP_NAMESPACE", "default"),
        runtime_class=os.environ.get("AXERN_STARTUP_RUNTIME_CLASS", "runsc"),
        request_cpu=os.environ.get("AXERN_STARTUP_REQUEST_CPU", "100m"),
        request_memory=os.environ.get("AXERN_STARTUP_REQUEST_MEMORY", "128Mi"),
        limit_cpu=os.environ.get("AXERN_STARTUP_LIMIT_CPU", "500m"),
        limit_memory=os.environ.get("AXERN_STARTUP_LIMIT_MEMORY", "512Mi"),
        node_selector=parse_string_map_json(
            os.environ.get("AXERN_STARTUP_NODE_SELECTOR_JSON", "{}"),
            "AXERN_STARTUP_NODE_SELECTOR_JSON",
        ),
        grouped_replicas_per_service=positive_int(
            os.environ.get("AXERN_STARTUP_GROUPED_REPLICAS_PER_SERVICE", "4"),
            "AXERN_STARTUP_GROUPED_REPLICAS_PER_SERVICE",
        ),
        stages=parse_int_list(os.environ.get("AXERN_STARTUP_STAGES", "1,6,12")),
        iterations=positive_int(os.environ.get("AXERN_STARTUP_ITERATIONS", "1"), "AXERN_STARTUP_ITERATIONS"),
        phases=phases,
        prometheus_url=os.environ.get("AXERN_STARTUP_PROMETHEUS_URL", "").strip().rstrip("/"),
        metrics_timeout_seconds=positive_float(
            os.environ.get("AXERN_STARTUP_METRICS_TIMEOUT_SECONDS", "45"),
            "AXERN_STARTUP_METRICS_TIMEOUT_SECONDS",
        ),
        metrics_poll_interval_seconds=positive_float(
            os.environ.get("AXERN_STARTUP_METRICS_POLL_INTERVAL_SECONDS", "1"),
            "AXERN_STARTUP_METRICS_POLL_INTERVAL_SECONDS",
        ),
        required_counter_metrics=required_counter_metrics,
        ready_timeout_seconds=positive_float(
            os.environ.get("AXERN_STARTUP_READY_TIMEOUT_SECONDS", "300"),
            "AXERN_STARTUP_READY_TIMEOUT_SECONDS",
        ),
        stage_pause_seconds=nonnegative_float(
            os.environ.get("AXERN_STARTUP_STAGE_PAUSE_SECONDS", "5"),
            "AXERN_STARTUP_STAGE_PAUSE_SECONDS",
        ),
        scenarios=scenarios,
    )


def parse_string_map_json(value: str, name: str) -> dict[str, str]:
    try:
        parsed = json.loads(value)
    except json.JSONDecodeError as exc:
        raise SystemExit(f"{name} must be a JSON object with string keys and values: {exc}") from exc
    if not isinstance(parsed, dict) or any(
        not isinstance(key, str) or not isinstance(item, str)
        for key, item in parsed.items()
    ):
        raise SystemExit(f"{name} must be a JSON object with string keys and values")
    normalized: dict[str, str] = {}
    for key, item in parsed.items():
        key = key.strip()
        item = item.strip()
        if not key or not item:
            raise SystemExit(f"{name} keys and values must be non-empty after trimming")
        if key in normalized:
            raise SystemExit(f"{name} contains duplicate key after trimming: {key}")
        normalized[key] = item
    return normalized


def load_scenarios(path: str, selected: tuple[str, ...]) -> tuple[Scenario, ...]:
    data = json.loads(Path(path).read_text())
    selected_set = set(selected)
    scenarios: list[Scenario] = []
    for raw in data.get("scenarios", []):
        name = str(raw.get("name", "")).strip()
        if not name:
            raise SystemExit(f"{path} contains a scenario without name")
        if selected_set and name not in selected_set:
            continue
        scenarios.append(Scenario(
            name=name,
            image_ref=required_field(raw, "image_ref", name),
            port=int(raw.get("port", 8080)),
            path=str(raw.get("path", "/")),
            expected_body=str(raw.get("expected_body", "")),
            sandbox_argv=parse_argv(raw, "sandbox_argv", name),
            service_argv=parse_argv(raw, "service_argv", name),
        ))
    if selected_set:
        found = {scenario.name for scenario in scenarios}
        missing = sorted(selected_set - found)
        if missing:
            raise SystemExit(f"unknown AXERN_STARTUP_SCENARIOS value(s): {', '.join(missing)}")
    if not scenarios:
        raise SystemExit(f"{path} did not produce any scenarios")
    return tuple(scenarios)


def required_field(raw: dict[str, Any], field: str, scenario: str) -> str:
    value = str(raw.get(field, "")).strip()
    if not value:
        raise SystemExit(f"scenario {scenario} missing required field: {field}")
    return value


def parse_argv(raw: dict[str, Any], field: str, scenario: str) -> tuple[str, ...]:
    value = raw.get(field, [])
    if not isinstance(value, list) or not all(isinstance(item, str) and item for item in value):
        raise SystemExit(f"scenario {scenario} field {field} must be a non-empty string array")
    return tuple(value)


def resolve_local_path(value: str) -> str:
    path = Path(value).expanduser()
    if path.is_absolute() or path.exists():
        return str(path)
    parent_path = Path("..") / path
    if parent_path.exists():
        return str(parent_path)
    return str(path)


def parse_int_list(value: str) -> tuple[int, ...]:
    values = tuple(int(part.strip()) for part in value.split(",") if part.strip())
    if not values or any(item <= 0 for item in values):
        raise SystemExit("AXERN_STARTUP_STAGES must contain positive integers")
    return values


def parse_str_list(value: str, name: str) -> tuple[str, ...]:
    values = tuple(part.strip() for part in value.split(",") if part.strip())
    if not values:
        raise SystemExit(f"{name} must not be empty")
    return values


def parse_optional_str_list(value: str) -> tuple[str, ...]:
    return tuple(part.strip() for part in value.split(",") if part.strip())


def positive_int(value: str, name: str) -> int:
    parsed = int(value)
    if parsed <= 0:
        raise SystemExit(f"{name} must be a positive integer")
    return parsed


def positive_float(value: str, name: str) -> float:
    parsed = float(value)
    if not math.isfinite(parsed) or parsed <= 0:
        raise SystemExit(f"{name} must be greater than zero")
    return parsed


def nonnegative_float(value: str, name: str) -> float:
    parsed = float(value)
    if not math.isfinite(parsed) or parsed < 0:
        raise SystemExit(f"{name} must not be negative")
    return parsed


def required_env(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise SystemExit(f"missing required env: {name}")
    return value


def sanitize_config(config: StartupConfig) -> dict[str, Any]:
    data = asdict(config)
    for key in ("tls_ca_cert", "tls_cert", "tls_key"):
        data[key] = "<set>" if data[key] else "<empty>"
    data["registry_credential_id"] = "<set>" if data["registry_credential_id"] else "<empty>"
    data["prometheus_url"] = "<set>" if data["prometheus_url"] else "<empty>"
    data["scenarios"] = [scenario.name for scenario in config.scenarios]
    return data


def capture_metrics(config: StartupConfig) -> MetricsCapture | None:
    if not config.prometheus_url:
        return None
    buckets: dict[str, list[dict[str, Any]]] = {}
    counters: dict[str, list[dict[str, Any]]] = {}
    gauges: dict[str, list[dict[str, Any]]] = {}
    errors: dict[str, str] = {}
    for spec in METRIC_SPECS:
        try:
            buckets[spec.name] = query_histogram_buckets(config.prometheus_url, spec)
        except Exception as exc:
            errors[spec.name] = f"{type(exc).__name__}: {exc}"
    for spec in COUNTER_SPECS:
        try:
            counters[spec.name] = query_counter_samples(config.prometheus_url, spec)
        except Exception as exc:
            errors[spec.name] = f"{type(exc).__name__}: {exc}"
    for spec in GAUGE_SPECS:
        try:
            gauges[spec.name] = query_instant_samples(config.prometheus_url, spec)
        except Exception as exc:
            errors[spec.name] = f"{type(exc).__name__}: {exc}"
    return MetricsCapture(
        captured_at=time.time(),
        buckets=buckets,
        counters=counters,
        gauges=gauges,
        errors=errors,
    )


def capture_metrics_before(config: StartupConfig) -> MetricsCapture | None:
    capture = capture_metrics(config)
    if capture is None or not capture.errors:
        return capture
    emit("metrics_capture_failure", {
        "scope": "before",
        "errors": capture.errors,
    })
    raise SystemExit("cannot capture complete Prometheus baseline")


def emit_metrics_summary(
    config: StartupConfig,
    scope: str,
    topology: str,
    scenario: str,
    stage: int,
    iteration: int,
    before: MetricsCapture | None,
) -> bool:
    if not config.prometheus_url or before is None:
        return True
    deadline = time.monotonic() + config.metrics_timeout_seconds
    after: MetricsCapture | None = None
    metrics: dict[str, Any] = {}
    errors: dict[str, str] = {}
    requirements: dict[str, dict[str, float | bool]] = {}
    while True:
        after = capture_metrics(config)
        if after is None:
            return True
        metrics, errors = summarize_metrics_delta(before, after)
        service_request_count = None
        if scope == "service_stage":
            service_request_count, _ = service_topology_shape(
                topology,
                stage,
                config.grouped_replicas_per_service,
            )
        requirements = metrics_requirements(
            scope,
            stage,
            metrics,
            service_request_count=service_request_count,
            required_counter_metrics=config.required_counter_metrics,
        )
        complete = requirements_complete(requirements) and not errors
        if complete or time.monotonic() >= deadline:
            break
        time.sleep(config.metrics_poll_interval_seconds)

    emit("metrics_summary", {
        "scope": scope,
        "topology": topology,
        "scenario": scenario,
        "stage": stage,
        "iteration": iteration,
        "start_ts": before.captured_at,
        "end_ts": after.captured_at,
        "elapsed_seconds": round(after.captured_at - before.captured_at, 3),
        "complete": complete,
        "requirements": requirements,
        "metrics": metrics,
        "errors": errors,
    })
    return complete


def summarize_metrics_delta(
    before: MetricsCapture,
    after: MetricsCapture,
) -> tuple[dict[str, Any], dict[str, str]]:
    metrics: dict[str, Any] = {}
    for spec in METRIC_SPECS:
        if spec.name in before.errors or spec.name in after.errors:
            metrics[spec.name] = []
            continue
        metrics[spec.name] = summarize_histogram_delta(
            before.buckets.get(spec.name, []),
            after.buckets.get(spec.name, []),
            spec,
        )
    for spec in COUNTER_SPECS:
        if spec.name in before.errors or spec.name in after.errors:
            metrics[spec.name] = []
            continue
        metrics[spec.name] = summarize_counter_delta(
            before.counters.get(spec.name, []),
            after.counters.get(spec.name, []),
            spec,
        )
    for spec in GAUGE_SPECS:
        if spec.name in after.errors:
            metrics[spec.name] = []
            continue
        metrics[spec.name] = summarize_gauge_samples(
            after.gauges.get(spec.name, []),
            spec,
        )
    errors = prefixed_errors("before", before.errors)
    errors.update(prefixed_errors("after", after.errors))
    return metrics, errors


def metrics_requirements(
    scope: str,
    stage: int,
    metrics: dict[str, Any],
    *,
    service_request_count: int | None = None,
    required_counter_metrics: tuple[str, ...] = (),
) -> dict[str, dict[str, float | bool]]:
    scope_contracts = {
        # The Python SDK Sandbox helper is backed by a transient Service.
        "sandbox_stage": {"owner_type": "service"},
        "service_stage": {"owner_type": "service"},
    }
    if scope not in scope_contracts:
        raise ValueError(f"unsupported metrics scope: {scope}")
    contract = scope_contracts[scope]

    observed = {
        "controld_resource_admission": metric_sample_count(
            metrics,
            "controld_resource_admission_stage",
            axern_owner_type=contract["owner_type"],
            axern_stage="total",
        ),
        "controld_replica_ready": metric_sample_count(metrics, "controld_service_replica_ready"),
        "axnoded_runtime_launch": metric_sample_count(
            metrics,
            "axnoded_startup_phase",
            axern_phase="runtime_launch",
        ),
    }
    if scope == "service_stage":
        observed.update({
            "controld_allocation_claim_wait": metric_sample_count(
                metrics,
                "controld_service_allocation_queue",
                axern_path="reconcile_create",
                axern_stage="claim_wait",
                axern_result="ok",
            ),
            "controld_allocation_dispatcher_wait": metric_sample_count(
                metrics,
                "controld_service_allocation_queue",
                axern_path="reconcile_create",
                axern_stage="dispatcher_wait",
                axern_result="ok",
            ),
            "controld_allocation_queue_total": metric_sample_count(
                metrics,
                "controld_service_allocation_queue",
                axern_path="reconcile_create",
                axern_stage="total",
                axern_result="ok",
            ),
            "axnoded_readiness_wait": metric_sample_count(
                metrics,
                "axnoded_readiness_wait",
                axern_probe_type="http",
            ),
            "axnoded_readiness_sandbox_stage": metric_sample_count(
                metrics,
                "axnoded_readiness_probe_stage",
                axern_probe_type="http",
                axern_stage="sandbox",
            ),
            "axnoded_readiness_external_port_stage": metric_sample_count(
                metrics,
                "axnoded_readiness_probe_stage",
                axern_probe_type="http",
                axern_stage="external_port",
            ),
            "gateway_service_proxy_total": metric_sample_count(
                metrics,
                "gateway_service_proxy_stage",
                axern_stage="total",
            ),
            "axnoded_http_proxy_total": metric_sample_count(
                metrics,
                "axnoded_http_proxy_stage",
                axern_stage="total",
            ),
            "axnoded_execution_lease_visibility": metric_sample_count(
                metrics,
                "axnoded_execution_lease_visibility",
            ),
        })
    expected = {name: stage for name in observed}
    if scope == "service_stage":
        if service_request_count is None or service_request_count <= 0:
            raise ValueError("service_request_count must be positive for service_stage metrics")
        for name in (
            "gateway_service_proxy_total",
            "axnoded_http_proxy_total",
            "axnoded_execution_lease_visibility",
        ):
            expected[name] = service_request_count
    for name in required_counter_metrics:
        requirement_name = f"counter_delta.{name}"
        observed[requirement_name] = metric_counter_value(metrics, name)
        expected[requirement_name] = 1
    return {
        name: {
            "expected": float(expected[name]),
            "observed": value,
            "complete": value >= expected[name],
        }
        for name, value in observed.items()
    }


def metric_sample_count(metrics: dict[str, Any], name: str, **required_labels: str) -> float:
    total = 0.0
    for summary in metrics.get(name, []):
        labels = summary.get("labels", {})
        if any(labels.get(key) != value for key, value in required_labels.items()):
            continue
        total += float(summary.get("samples", 0.0))
    return round(total, 3)


def metric_counter_value(metrics: dict[str, Any], name: str) -> float:
    return round(sum(float(summary.get("value", 0.0)) for summary in metrics.get(name, [])), 3)


def requirements_complete(requirements: dict[str, dict[str, float | bool]]) -> bool:
    return bool(requirements) and all(bool(requirement["complete"]) for requirement in requirements.values())


def prefixed_errors(prefix: str, errors: dict[str, str]) -> dict[str, str]:
    return {f"{prefix}.{name}": error for name, error in errors.items()}


def query_histogram_buckets(base_url: str, spec: MetricSpec) -> list[dict[str, Any]]:
    all_groups = (*spec.groups, *spec.series_groups)
    group_by = ", ".join(("le", *all_groups))
    samples = query_prometheus(base_url, f"sum({prometheus_selector(spec.metric, spec.matchers)}) by ({group_by})")
    series: dict[tuple[str, ...], dict[str, Any]] = {}
    for sample in samples:
        labels = {str(key): str(value) for key, value in sample.get("labels", {}).items()}
        le = labels.pop("le", "")
        if not le:
            continue
        group_labels = {group: labels.get(group, "") for group in all_groups}
        key = tuple(group_labels[group] for group in all_groups)
        entry = series.setdefault(key, {"labels": group_labels, "buckets": {}})
        entry["buckets"][le] = float(sample.get("value", 0.0))
    return list(series.values())


def query_counter_samples(base_url: str, spec: CounterSpec) -> list[dict[str, Any]]:
    all_groups = (*spec.groups, *spec.series_groups)
    selector = prometheus_selector(spec.metric, spec.matchers)
    expression = f"sum({selector})"
    if all_groups:
        expression += f" by ({', '.join(all_groups)})"
    samples = query_prometheus(base_url, expression)
    return [
        {
            "labels": {group: str(sample.get("labels", {}).get(group, "")) for group in all_groups},
            "value": float(sample.get("value", 0.0)),
        }
        for sample in samples
    ]


def query_instant_samples(base_url: str, spec: GaugeSpec) -> list[dict[str, Any]]:
    all_groups = (*spec.groups, *spec.series_groups)
    selector = prometheus_selector(spec.metric, spec.matchers)
    expression = f"sum({selector})"
    if all_groups:
        expression += f" by ({', '.join(all_groups)})"
    samples = query_prometheus(base_url, expression)
    return [
        {
            "labels": {group: str(sample.get("labels", {}).get(group, "")) for group in all_groups},
            "value": float(sample.get("value", 0.0)),
        }
        for sample in samples
    ]


def prometheus_selector(metric: str, matchers: tuple[tuple[str, str], ...]) -> str:
    if not matchers:
        return metric
    labels = ",".join(f"{name}={json.dumps(value)}" for name, value in matchers)
    return f"{metric}{{{labels}}}"


def summarize_histogram_delta(before: list[dict[str, Any]], after: list[dict[str, Any]], spec: MetricSpec) -> list[dict[str, Any]]:
    before_by_key = histogram_series_by_key(before, spec)
    after_by_key = histogram_series_by_key(after, spec)
    aggregated: dict[tuple[str, ...], dict[str, Any]] = {}
    for key in sorted(after_by_key):
        after_entry = after_by_key[key]
        before_entry = before_by_key.get(key, {"labels": after_entry["labels"], "buckets": {}})
        delta = delta_buckets(before_entry["buckets"], after_entry["buckets"])
        labels = {group: after_entry["labels"].get(group, "") for group in spec.groups}
        aggregate_key = tuple(labels[group] for group in spec.groups)
        entry = aggregated.setdefault(aggregate_key, {"labels": labels, "buckets": {}})
        for le, value in delta.items():
            entry["buckets"][le] = entry["buckets"].get(le, 0.0) + value

    summaries: list[dict[str, Any]] = []
    for key in sorted(aggregated):
        entry = aggregated[key]
        quantiles = histogram_quantiles(entry["buckets"])
        if not quantiles:
            continue
        summaries.append({
            "labels": entry["labels"],
            **quantiles,
        })
    return summaries


def histogram_series_by_key(series: list[dict[str, Any]], spec: MetricSpec) -> dict[tuple[str, ...], dict[str, Any]]:
    all_groups = (*spec.groups, *spec.series_groups)
    out: dict[tuple[str, ...], dict[str, Any]] = {}
    for entry in series:
        labels = {group: str(entry.get("labels", {}).get(group, "")) for group in all_groups}
        key = tuple(labels[group] for group in all_groups)
        out[key] = {
            "labels": labels,
            "buckets": {str(le): float(value) for le, value in entry.get("buckets", {}).items()},
        }
    return out


def summarize_counter_delta(
    before: list[dict[str, Any]],
    after: list[dict[str, Any]],
    spec: CounterSpec,
) -> list[dict[str, Any]]:
    before_by_key = counter_series_by_key(before, spec)
    after_by_key = counter_series_by_key(after, spec)
    aggregated: dict[tuple[str, ...], dict[str, Any]] = {}
    for key in sorted(after_by_key):
        after_entry = after_by_key[key]
        labels = {group: after_entry["labels"].get(group, "") for group in spec.groups}
        aggregate_key = tuple(labels[group] for group in spec.groups)
        entry = aggregated.setdefault(aggregate_key, {"labels": labels, "value": 0.0})
        before_value = before_by_key.get(key, {}).get("value", 0.0)
        entry["value"] += counter_delta(float(before_value), after_entry["value"])
    return [
        {"labels": aggregated[key]["labels"], "value": round(aggregated[key]["value"], 3)}
        for key in sorted(aggregated)
        if aggregated[key]["value"] > 0
    ]


def counter_series_by_key(
    series: list[dict[str, Any]],
    spec: CounterSpec,
) -> dict[tuple[str, ...], dict[str, Any]]:
    all_groups = (*spec.groups, *spec.series_groups)
    out: dict[tuple[str, ...], dict[str, Any]] = {}
    for entry in series:
        labels = {group: str(entry.get("labels", {}).get(group, "")) for group in all_groups}
        key = tuple(labels[group] for group in all_groups)
        out[key] = {"labels": labels, "value": float(entry.get("value", 0.0))}
    return out


def summarize_gauge_samples(
    samples: list[dict[str, Any]],
    spec: GaugeSpec,
) -> list[dict[str, Any]]:
    aggregated: dict[tuple[str, ...], dict[str, Any]] = {}
    for sample in samples:
        labels = {group: str(sample.get("labels", {}).get(group, "")) for group in spec.groups}
        key = tuple(labels[group] for group in spec.groups)
        entry = aggregated.setdefault(key, {"labels": labels, "value": 0.0})
        entry["value"] += float(sample.get("value", 0.0))
    return [
        {"labels": aggregated[key]["labels"], "value": round(aggregated[key]["value"], 3)}
        for key in sorted(aggregated)
    ]


def delta_buckets(before: dict[str, float], after: dict[str, float]) -> dict[str, float]:
    labels = set(before) | set(after)
    return {
        le: counter_delta(before.get(le, 0.0), after.get(le, 0.0))
        for le in labels
    }


def counter_delta(before: float, after: float) -> float:
    if after >= before:
        return after - before
    return after


def histogram_quantiles(buckets: dict[str, float]) -> dict[str, float]:
    cumulative = monotonic_buckets(buckets)
    if not cumulative:
        return {}
    total = cumulative[-1][1]
    if total <= 0:
        return {}
    summary = {"samples": round(total, 3)}
    for quantile in METRIC_QUANTILES:
        summary[f"p{int(quantile * 100)}"] = round(histogram_quantile(quantile, cumulative, total), 6)
    return summary


def monotonic_buckets(buckets: dict[str, float]) -> list[tuple[float, float]]:
    parsed = sorted((bucket_bound(le), value) for le, value in buckets.items())
    cumulative: list[tuple[float, float]] = []
    last = 0.0
    for bound, value in parsed:
        value = max(value, last)
        cumulative.append((bound, value))
        last = value
    return cumulative


def bucket_bound(le: str) -> float:
    if le in {"+Inf", "Inf", "inf"}:
        return math.inf
    return float(le)


def histogram_quantile(quantile: float, cumulative: list[tuple[float, float]], total: float) -> float:
    rank = quantile * total
    lower_bound = 0.0
    lower_count = 0.0
    for upper_bound, upper_count in cumulative:
        if upper_count < rank:
            if not math.isinf(upper_bound):
                lower_bound = upper_bound
            lower_count = upper_count
            continue
        if math.isinf(upper_bound):
            return lower_bound
        bucket_count = upper_count - lower_count
        if bucket_count <= 0:
            return upper_bound
        position = (rank - lower_count) / bucket_count
        return lower_bound + (upper_bound - lower_bound) * position
    return lower_bound


def query_prometheus(base_url: str, query: str) -> list[dict[str, Any]]:
    url = f"{base_url}/api/v1/query?{urllib.parse.urlencode({'query': query})}"
    with urllib.request.urlopen(url, timeout=10.0) as response:
        payload = json.loads(response.read().decode("utf-8"))
    if payload.get("status") != "success":
        raise RuntimeError(payload)
    output = []
    for item in payload.get("data", {}).get("result", []):
        value = item.get("value", [None, ""])
        try:
            numeric = float(value[1])
        except (TypeError, ValueError, IndexError):
            numeric = value[1] if len(value) > 1 else ""
        output.append({
            "labels": item.get("metric", {}),
            "value": numeric,
        })
    return output


def emit(event: str, payload: dict[str, Any]) -> None:
    data = {"event": event, "ts": time.time_ns(), **payload}
    line = json.dumps(data, sort_keys=True)
    with _EMIT_LOCK:
        print(line, flush=True)


def disable_proxy_env() -> None:
    for name in (
        "HTTP_PROXY",
        "HTTPS_PROXY",
        "http_proxy",
        "https_proxy",
        "ALL_PROXY",
        "NO_PROXY",
        "all_proxy",
        "no_proxy",
    ):
        os.environ.pop(name, None)


def truthy(value: str) -> bool:
    return value.strip().lower() not in {"", "0", "false", "n", "no", "off"}


if __name__ == "__main__":
    main()
