from __future__ import annotations

import asyncio
from contextlib import aclosing
import math
import os
import statistics
import uuid
from dataclasses import asdict, dataclass, field

import grpc

from axern.control.common.v1 import common_pb2
from axern.control.service.v1 import service_types_pb2
from axern_sdk import HTTPProbe, ServiceProbe
from axern_sdk.async_client import AsyncAxernClient

import readiness


@dataclass(frozen=True)
class LifecycleResult:
    ok: bool
    scenario: str
    arrival_rate: float
    index: int
    schedule_lag_seconds: float
    create_ack_seconds: float = 0.0
    ready_seconds: float = 0.0
    first_http_seconds: float = 0.0
    lifecycle_seconds: float = 0.0
    service_id: str = ""
    node_id: str = ""
    error_stage: str = ""
    error: str = ""


@dataclass
class SteadyAccumulator:
    scheduled: int = 0
    ok: int = 0
    failed: int = 0
    error_stages: dict[str, int] = field(default_factory=dict)
    scenario_counts: dict[str, int] = field(default_factory=dict)
    node_counts: dict[str, int] = field(default_factory=dict)
    schedule_lag_seconds: list[float] = field(default_factory=list)
    create_ack_seconds: list[float] = field(default_factory=list)
    ready_seconds: list[float] = field(default_factory=list)
    first_http_seconds: list[float] = field(default_factory=list)

    def record(self, result: LifecycleResult) -> None:
        self.scheduled += 1
        self.schedule_lag_seconds.append(result.schedule_lag_seconds)
        self.scenario_counts[result.scenario] = self.scenario_counts.get(result.scenario, 0) + 1
        if result.node_id:
            self.node_counts[result.node_id] = self.node_counts.get(result.node_id, 0) + 1
        if result.ok:
            self.ok += 1
            self.create_ack_seconds.append(result.create_ack_seconds)
            self.ready_seconds.append(result.ready_seconds)
            self.first_http_seconds.append(result.first_http_seconds)
            return
        self.failed += 1
        if result.error_stage:
            self.error_stages[result.error_stage] = self.error_stages.get(result.error_stage, 0) + 1


async def main_async() -> None:
    readiness.disable_proxy_env()
    config = readiness.config_from_env()
    rates = parse_rates(os.environ.get("AXERN_STEADY_ARRIVAL_RATES", "1,2,4"))
    duration = readiness.positive_float(
        os.environ.get("AXERN_STEADY_DURATION_SECONDS", "120"),
        "AXERN_STEADY_DURATION_SECONDS",
    )
    lifetime = readiness.positive_float(
        os.environ.get("AXERN_STEADY_SERVICE_LIFETIME_SECONDS", "30"),
        "AXERN_STEADY_SERVICE_LIFETIME_SECONDS",
    )
    max_inflight = readiness.positive_int(
        os.environ.get("AXERN_STEADY_MAX_INFLIGHT", "1024"),
        "AXERN_STEADY_MAX_INFLIGHT",
    )
    max_schedule_lag = readiness.positive_float(
        os.environ.get("AXERN_STEADY_MAX_SCHEDULE_LAG_SECONDS", "0.25"),
        "AXERN_STEADY_MAX_SCHEDULE_LAG_SECONDS",
    )
    client = AsyncAxernClient(
        config.endpoint,
        tls_ca_cert=config.tls_ca_cert,
        tls_cert=config.tls_cert,
        tls_key=config.tls_key,
    )
    failures = 0
    try:
        for rate in rates:
            summary = await run_rate(
                client,
                config,
                rate,
                duration,
                lifetime,
                max_inflight,
                max_schedule_lag,
            )
            failures += summary.failed
            emit_summary(rate, duration, lifetime, summary)
    finally:
        await client.close()
    if failures:
        raise SystemExit(1)


async def run_rate(
    client: AsyncAxernClient,
    config: readiness.StartupConfig,
    rate: float,
    duration: float,
    lifetime: float,
    max_inflight: int,
    max_schedule_lag: float,
) -> SteadyAccumulator:
    run_id = os.environ.get("AXERN_STEADY_RUN_ID", "").strip() or uuid.uuid4().hex
    environments: dict[str, str] = {}
    tasks: set[asyncio.Task[LifecycleResult]] = set()
    summary = SteadyAccumulator()

    def record(result: LifecycleResult) -> None:
        summary.record(result)
        readiness.emit("steady_lifecycle", asdict(result))
    try:
        for scenario in config.scenarios:
            environment = await client.create_environment(
                namespace=config.namespace,
                image_ref=scenario.image_ref,
                registry_credential_id=config.registry_credential_id,
                labels=load_labels(run_id, scenario.name, rate),
                timeout=30.0,
            )
            environments[scenario.name] = environment.id

        sample_count = max(1, math.floor(rate * duration))
        interval = 1.0 / rate
        started = asyncio.get_running_loop().time()
        readiness.emit("steady_rate_start", {
            "arrival_rate": rate,
            "duration_seconds": duration,
            "lifetime_seconds": lifetime,
            "scheduled": sample_count,
            "scenarios": [scenario.name for scenario in config.scenarios],
        })
        for index in range(sample_count):
            scheduled_at = started + index * interval
            await asyncio.sleep(max(0.0, scheduled_at - asyncio.get_running_loop().time()))
            completed = {task for task in tasks if task.done()}
            tasks.difference_update(completed)
            for task in completed:
                record(task.result())
            if len(tasks) >= max_inflight:
                result = LifecycleResult(
                    ok=False,
                    scenario=config.scenarios[index % len(config.scenarios)].name,
                    arrival_rate=rate,
                    index=index,
                    schedule_lag_seconds=round(asyncio.get_running_loop().time() - scheduled_at, 6),
                    error_stage="harness_backpressure",
                    error=f"in-flight lifecycle tasks reached AXERN_STEADY_MAX_INFLIGHT={max_inflight}",
                )
                record(result)
                continue
            scenario = config.scenarios[index % len(config.scenarios)]
            tasks.add(asyncio.create_task(
                run_lifecycle(
                    client,
                    config,
                    scenario,
                    environments[scenario.name],
                    run_id,
                    rate,
                    index,
                    scheduled_at,
                    lifetime,
                    max_schedule_lag,
                )
            ))
        if tasks:
            for result in await asyncio.gather(*tasks):
                record(result)
            tasks.clear()
    finally:
        for task in tasks:
            if not task.done():
                task.cancel()
        if tasks:
            for outcome in await asyncio.gather(*tasks, return_exceptions=True):
                if isinstance(outcome, LifecycleResult):
                    record(outcome)
            tasks.clear()
        for environment_id in environments.values():
            try:
                await client.delete_environment(environment_id, timeout=30.0)
            except Exception as exc:
                result = LifecycleResult(
                    ok=False,
                    scenario="",
                    arrival_rate=rate,
                    index=summary.scheduled,
                    schedule_lag_seconds=0.0,
                    error_stage="environment_cleanup",
                    error=f"{type(exc).__name__}: {exc}",
                )
                record(result)
                readiness.emit("failure", {
                    "phase": "steady_environment_cleanup",
                    "environment_id": environment_id,
                    "error": result.error,
                })
    return summary


async def run_lifecycle(
    client: AsyncAxernClient,
    config: readiness.StartupConfig,
    scenario: readiness.Scenario,
    environment_id: str,
    run_id: str,
    rate: float,
    index: int,
    scheduled_at: float,
    lifetime: float,
    max_schedule_lag: float,
) -> LifecycleResult:
    loop = asyncio.get_running_loop()
    started = loop.time()
    schedule_lag = started - scheduled_at
    service_id = ""
    node_id = ""
    stage = "schedule"
    create_ack = 0.0
    ready = 0.0
    first_http = 0.0
    try:
        if schedule_lag > max_schedule_lag:
            raise RuntimeError(
                f"schedule lag {schedule_lag:.6f}s exceeded {max_schedule_lag:.6f}s"
            )
        stage = "create"
        service = await client.create_service(
            namespace=config.namespace,
            environment_id=environment_id,
            replicas=1,
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
            labels=load_labels(run_id, scenario.name, rate),
            timeout=120.0,
        )
        service_id = service.id
        create_ack = loop.time() - started
        stage = "ready"
        node_id = await wait_ready(client, service_id, config.ready_timeout_seconds)
        ready = loop.time() - started
        stage = "first_http"
        http_result = await asyncio.to_thread(
            readiness.fetch_first_gateway_http,
            config,
            scenario,
            "steady-state",
            service_id,
            round(rate),
            1,
            index,
            started,
        )
        if not http_result.ok:
            raise RuntimeError(http_result.error)
        first_http = loop.time() - started
        stage = "lifetime"
        await asyncio.sleep(lifetime)
        stage = "cleanup"
        await cleanup_service(client, service_id)
        service_id = ""
        return LifecycleResult(
            ok=True,
            scenario=scenario.name,
            arrival_rate=rate,
            index=index,
            schedule_lag_seconds=round(schedule_lag, 6),
            create_ack_seconds=round(create_ack, 6),
            ready_seconds=round(ready, 6),
            first_http_seconds=round(first_http, 6),
            lifecycle_seconds=round(loop.time() - started, 6),
            node_id=node_id,
        )
    except Exception as exc:
        return LifecycleResult(
            ok=False,
            scenario=scenario.name,
            arrival_rate=rate,
            index=index,
            schedule_lag_seconds=round(schedule_lag, 6),
            create_ack_seconds=round(create_ack, 6),
            ready_seconds=round(ready, 6),
            first_http_seconds=round(first_http, 6),
            lifecycle_seconds=round(loop.time() - started, 6),
            service_id=service_id,
            node_id=node_id,
            error_stage=stage,
            error=f"{type(exc).__name__}: {exc}",
        )
    finally:
        if service_id:
            try:
                await cleanup_service(client, service_id)
            except Exception as exc:
                readiness.emit("failure", {
                    "phase": "steady_service_cleanup",
                    "service_id": service_id,
                    "error": f"{type(exc).__name__}: {exc}",
                })


async def wait_ready(client: AsyncAxernClient, service_id: str, timeout_seconds: float) -> str:
    deadline = asyncio.get_running_loop().time() + timeout_seconds
    async with aclosing(client.watch_service(service_id, timeout=timeout_seconds)) as watch:
        async for service in watch:
            remaining = deadline - asyncio.get_running_loop().time()
            replicas = await client.list_service_replicas(
                service_id,
                timeout=min(10.0, max(0.1, remaining)),
            )
            ready = [replica for replica in replicas if readiness.is_ready_replica(replica)]
            if service.status == service_types_pb2.SERVICE_STATUS_READY and len(ready) == 1:
                return ready[0].node_id
            if service.status in {
                service_types_pb2.SERVICE_STATUS_DEGRADED,
                service_types_pb2.SERVICE_STATUS_FAILED,
                service_types_pb2.SERVICE_STATUS_DELETING,
                service_types_pb2.SERVICE_STATUS_DELETED,
            }:
                diagnostic = common_pb2.WorkloadDiagnosticCode.Name(service.diagnostic_code)
                raise RuntimeError(
                    f"service became {service_types_pb2.ServiceStatus.Name(service.status)} "
                    f"diagnostic={diagnostic}: {service.message}"
                )
    raise TimeoutError(f"service {service_id} did not become ready within {timeout_seconds}s")


async def cleanup_service(client: AsyncAxernClient, service_id: str) -> None:
    await client.delete_service(service_id, timeout=30.0)
    deadline = asyncio.get_running_loop().time() + 180.0
    while True:
        try:
            await client.admin_purge_service(
                service_id,
                operator_reason="steady state benchmark cleanup",
                timeout=15.0,
            )
            return
        except grpc.aio.AioRpcError as exc:
            if exc.code() != grpc.StatusCode.FAILED_PRECONDITION or asyncio.get_running_loop().time() >= deadline:
                raise
        await asyncio.sleep(1.0)


def load_labels(run_id: str, scenario: str, rate: float) -> dict[str, str]:
    return {
        "axern.load": "steady-state",
        "axern.load.run": run_id,
        "axern.load.scenario": scenario,
        "axern.load.rate": str(rate),
    }


def emit_summary(rate: float, duration: float, lifetime: float, summary: SteadyAccumulator) -> None:
    readiness.emit("steady_summary", {
        "arrival_rate": rate,
        "duration_seconds": duration,
        "lifetime_seconds": lifetime,
        "scheduled": summary.scheduled,
        "ok": summary.ok,
        "failed": summary.failed,
        "error_stages": dict(sorted(summary.error_stages.items())),
        "scenario_counts": dict(sorted(summary.scenario_counts.items())),
        "node_counts": dict(sorted(summary.node_counts.items())),
        "schedule_lag_seconds": summarize(summary.schedule_lag_seconds),
        "create_ack_seconds": summarize(summary.create_ack_seconds),
        "ready_seconds": summarize(summary.ready_seconds),
        "first_http_seconds": summarize(summary.first_http_seconds),
    })


def summarize(values: list[float]) -> dict[str, float]:
    if not values:
        return {}
    ordered = sorted(values)
    return {
        "p50": round(statistics.median(ordered), 6),
        "p95": round(readiness.percentile(ordered, 0.95), 6),
        "p99": round(readiness.percentile(ordered, 0.99), 6),
        "max": round(ordered[-1], 6),
    }


def parse_rates(value: str) -> tuple[float, ...]:
    rates = tuple(float(part.strip()) for part in value.split(",") if part.strip())
    if not rates or any(not math.isfinite(rate) or rate <= 0 for rate in rates):
        raise SystemExit("AXERN_STEADY_ARRIVAL_RATES must contain positive finite numbers")
    return rates


def main() -> None:
    asyncio.run(main_async())


if __name__ == "__main__":
    main()
