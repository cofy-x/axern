from __future__ import annotations

import concurrent.futures
import os
import threading
import time
import uuid
from dataclasses import asdict, dataclass, replace
from typing import Any

from axern.control.common.v1 import common_pb2
from axern.control.service.v1 import service_types_pb2
from axern_sdk import HTTPProbe, ServiceProbe

import readiness


@dataclass(frozen=True)
class CapacityOutcome:
    service_id: str
    outcome: str
    elapsed_seconds: float
    status: str = ""
    diagnostic_code: str = ""
    message: str = ""
    node_id: str = ""


def main() -> None:
    readiness.disable_proxy_env()
    config = readiness.config_from_env()
    if len(config.scenarios) != 1:
        raise SystemExit("capacity probe requires exactly one AXERN_STARTUP_SCENARIOS value")
    if len(config.stages) != 1:
        raise SystemExit("capacity probe requires exactly one AXERN_STARTUP_STAGES value")

    expectation = os.environ.get("AXERN_CAPACITY_EXPECTATION", "all_ready").strip()
    if expectation not in {"all_ready", "saturation"}:
        raise SystemExit("AXERN_CAPACITY_EXPECTATION must be all_ready or saturation")
    create_workers = readiness.positive_int(
        os.environ.get("AXERN_CAPACITY_CREATE_WORKERS", "256"),
        "AXERN_CAPACITY_CREATE_WORKERS",
    )
    poll_interval = readiness.positive_float(
        os.environ.get("AXERN_CAPACITY_POLL_INTERVAL_SECONDS", "0.2"),
        "AXERN_CAPACITY_POLL_INTERVAL_SECONDS",
    )
    max_outcome_seconds = readiness.positive_float(
        os.environ.get("AXERN_CAPACITY_MAX_OUTCOME_SECONDS", "30"),
        "AXERN_CAPACITY_MAX_OUTCOME_SECONDS",
    )
    scenario = config.scenarios[0]
    stage = config.stages[0]
    run_id = uuid.uuid4().hex
    run_labels = {
        "axern.load": "capacity-probe",
        "axern.load.run": run_id,
        "axern.load.scenario": scenario.name,
        "axern.load.stage": str(stage),
    }
    readiness.emit("capacity_config", {
        "scenario": scenario.name,
        "stage": stage,
        "expectation": expectation,
        "create_workers": min(create_workers, stage),
        "ready_timeout_seconds": config.ready_timeout_seconds,
        "max_outcome_seconds": max_outcome_seconds,
    })

    client = readiness.control_client(config)
    environment_id = ""
    service_ids: list[str] = []
    outcomes: list[CapacityOutcome] = []
    try:
        environment = client.create_environment(
            namespace=config.namespace,
            image_ref=scenario.image_ref,
            registry_credential_id=config.registry_credential_id,
            labels=run_labels,
            timeout=30.0,
        )
        environment_id = environment.id
        started = time.monotonic()
        service_ids, create_failures = create_services(
            client,
            config,
            scenario,
            environment_id,
            run_labels,
            stage,
            min(create_workers, stage),
            started,
        )
        outcomes.extend(create_failures)
        outcomes.extend(
            attach_ready_nodes(
                client,
                wait_for_outcomes(
                    client,
                    config.namespace,
                    run_labels,
                    service_ids,
                    started,
                    config.ready_timeout_seconds,
                    poll_interval,
                ),
            )
        )
        emit_summary(stage, expectation, outcomes)
    finally:
        cleanup_failures = readiness.cleanup_service_stage(client, service_ids, environment_id)
        for resource_type, resource_id, error in cleanup_failures:
            readiness.emit("failure", {
                "phase": "capacity_cleanup",
                "resource_type": resource_type,
                "resource_id": resource_id,
                "error": error,
            })
        client.close()

    counts = outcome_counts(outcomes)
    valid = not cleanup_failures and counts.get("unexpected", 0) == 0 and counts.get("timeout", 0) == 0
    valid = valid and all(
        outcome.elapsed_seconds <= max_outcome_seconds
        for outcome in outcomes
        if outcome.outcome in {"ready", "admission_blocked"}
    )
    if expectation == "all_ready":
        valid = valid and counts.get("ready", 0) == stage and counts.get("admission_blocked", 0) == 0
    else:
        valid = valid and counts.get("ready", 0) > 0 and counts.get("admission_blocked", 0) > 0
    if not valid:
        raise SystemExit(1)


def create_services(
    client: Any,
    config: readiness.StartupConfig,
    scenario: readiness.Scenario,
    environment_id: str,
    labels: dict[str, str],
    stage: int,
    workers: int,
    started: float,
) -> tuple[list[str], list[CapacityOutcome]]:
    service_ids: list[str] = []
    failures: list[CapacityOutcome] = []
    start_gate = threading.Event()

    def create(index: int) -> tuple[str, CapacityOutcome | None]:
        del index
        start_gate.wait()
        try:
            service = client.create_service(
                namespace=config.namespace,
                environment_id=environment_id,
                replicas=1,
                argv=list(scenario.service_argv),
                runtime_class=config.runtime_class,
                request_cpu=config.request_cpu,
                request_memory=config.request_memory,
                limit_cpu=config.limit_cpu,
                limit_memory=config.limit_memory,
                readiness_probe=ServiceProbe(
                    http=HTTPProbe(port=scenario.port, path=scenario.path),
                    period=0.1,
                    timeout=1.0,
                    failure_threshold=3,
                ),
                labels=labels,
                timeout=120.0,
            )
            return service.id, None
        except Exception as exc:
            return "", CapacityOutcome(
                service_id="",
                outcome="unexpected",
                elapsed_seconds=round(time.monotonic() - started, 3),
                message=f"create service: {type(exc).__name__}: {exc}",
            )

    with concurrent.futures.ThreadPoolExecutor(max_workers=workers) as pool:
        futures = [pool.submit(create, index) for index in range(stage)]
        start_gate.set()
        for future in concurrent.futures.as_completed(futures):
            service_id, failure = future.result()
            if service_id:
                service_ids.append(service_id)
            if failure is not None:
                failures.append(failure)
                readiness.emit("capacity_outcome", asdict(failure))
    readiness.emit("capacity_create_complete", {
        "requested": stage,
        "created": len(service_ids),
        "failed": len(failures),
        "elapsed_seconds": round(time.monotonic() - started, 3),
    })
    return service_ids, failures


def wait_for_outcomes(
    client: Any,
    namespace: str,
    labels: dict[str, str],
    service_ids: list[str],
    started: float,
    timeout_seconds: float,
    poll_interval: float,
) -> list[CapacityOutcome]:
    pending = set(service_ids)
    outcomes: list[CapacityOutcome] = []
    deadline = time.monotonic() + timeout_seconds
    while pending and time.monotonic() < deadline:
        services = client.list_services(
            namespace=namespace,
            labels=labels,
            page_size=200,
            timeout=min(30.0, max(0.1, deadline - time.monotonic())),
        )
        for service in services:
            if service.id not in pending:
                continue
            outcome = classify_service(service, started)
            if outcome is None:
                continue
            pending.remove(service.id)
            outcomes.append(outcome)
            readiness.emit("capacity_outcome", asdict(outcome))
        if pending:
            time.sleep(min(poll_interval, max(0.0, deadline - time.monotonic())))

    elapsed = round(time.monotonic() - started, 3)
    for service_id in sorted(pending):
        outcome = CapacityOutcome(
            service_id=service_id,
            outcome="timeout",
            elapsed_seconds=elapsed,
            message=f"service did not reach ready or a structured terminal diagnostic within {timeout_seconds}s",
        )
        outcomes.append(outcome)
        readiness.emit("capacity_outcome", asdict(outcome))
    return outcomes


def classify_service(service: Any, started: float) -> CapacityOutcome | None:
    diagnostic = common_pb2.WorkloadDiagnosticCode.Name(service.diagnostic_code)
    status = service_types_pb2.ServiceStatus.Name(service.status)
    elapsed = round(time.monotonic() - started, 3)
    if service.status == service_types_pb2.SERVICE_STATUS_READY and service.ready_replicas == 1:
        return CapacityOutcome(
            service_id=service.id,
            outcome="ready",
            elapsed_seconds=elapsed,
            status=status,
            diagnostic_code=diagnostic,
        )
    if (
        service.status == service_types_pb2.SERVICE_STATUS_DEGRADED
        and service.diagnostic_code == common_pb2.WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED
    ):
        return CapacityOutcome(
            service_id=service.id,
            outcome="admission_blocked",
            elapsed_seconds=elapsed,
            status=status,
            diagnostic_code=diagnostic,
            message=service.message,
        )
    if service.status in {
        service_types_pb2.SERVICE_STATUS_FAILED,
        service_types_pb2.SERVICE_STATUS_DELETING,
        service_types_pb2.SERVICE_STATUS_DELETED,
    } or service.status == service_types_pb2.SERVICE_STATUS_DEGRADED:
        return CapacityOutcome(
            service_id=service.id,
            outcome="unexpected",
            elapsed_seconds=elapsed,
            status=status,
            diagnostic_code=diagnostic,
            message=service.message,
        )
    return None


def attach_ready_nodes(client: Any, outcomes: list[CapacityOutcome]) -> list[CapacityOutcome]:
    ready = [outcome for outcome in outcomes if outcome.outcome == "ready"]
    if not ready:
        return outcomes

    def node_id(service_id: str) -> tuple[str, str]:
        replicas = client.list_service_replicas(service_id, timeout=30.0)
        candidates = [replica for replica in replicas if readiness.is_ready_replica(replica)]
        if len(candidates) != 1:
            raise RuntimeError(f"ready service {service_id} has {len(candidates)} authoritative ready replicas")
        return service_id, candidates[0].node_id

    nodes: dict[str, str] = {}
    with concurrent.futures.ThreadPoolExecutor(max_workers=min(len(ready), 64)) as pool:
        futures = [pool.submit(node_id, outcome.service_id) for outcome in ready]
        for future in concurrent.futures.as_completed(futures):
            service_id, value = future.result()
            nodes[service_id] = value
    return [replace(outcome, node_id=nodes.get(outcome.service_id, outcome.node_id)) for outcome in outcomes]


def emit_summary(stage: int, expectation: str, outcomes: list[CapacityOutcome]) -> None:
    counts = outcome_counts(outcomes)
    latencies: dict[str, dict[str, float]] = {}
    for outcome in sorted(counts):
        values = [item.elapsed_seconds for item in outcomes if item.outcome == outcome]
        latencies[outcome] = readiness.latency_summary(values)
    node_counts: dict[str, int] = {}
    for outcome in outcomes:
        if outcome.node_id:
            node_counts[outcome.node_id] = node_counts.get(outcome.node_id, 0) + 1
    readiness.emit("capacity_summary", {
        "stage": stage,
        "expectation": expectation,
        "outcomes": counts,
        "latency_seconds": latencies,
        "node_counts": dict(sorted(node_counts.items())),
    })


def outcome_counts(outcomes: list[CapacityOutcome]) -> dict[str, int]:
    counts: dict[str, int] = {}
    for outcome in outcomes:
        counts[outcome.outcome] = counts.get(outcome.outcome, 0) + 1
    return dict(sorted(counts.items()))


if __name__ == "__main__":
    main()
