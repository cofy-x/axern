from __future__ import annotations

import json
import math
import os
from pathlib import Path
import signal
import ssl
import subprocess
import sys
import time
from typing import Any
import urllib.parse
import urllib.request


DEFAULT_QUERIES = {
    "allocation_queue": "sum(axern_controld_allocation_reconcile_queue_current)",
    "active_allocations": "sum(axern_controld_allocations_current)",
    "node_storage_used_bytes": (
        'sum by (axern_storage) (axern_controld_node_storage_current{axern_state="used"})'
    ),
}

_STOP_REQUESTED = False
_ACTIVE_RUN: subprocess.Popen[str] | None = None


def main() -> None:
    install_signal_handlers()
    duration_seconds = positive_float("AXERN_SOAK_DURATION_SECONDS", 3600.0)
    cohort_size = positive_int("AXERN_SOAK_COHORT_SIZE", 36)
    warmup_cohorts = nonnegative_int("AXERN_SOAK_WARMUP_COHORTS", 1)
    scenarios = required_list_env("AXERN_STARTUP_SCENARIOS")
    output_dir = Path(os.environ.get("AXERN_SOAK_OUTPUT_DIR", "work/axern-lifecycle-soak"))
    output_dir.mkdir(parents=True, exist_ok=True)

    prometheus_url = os.environ.get("AXERN_STARTUP_PROMETHEUS_URL", "").rstrip("/")
    if not prometheus_url:
        raise SystemExit("AXERN_STARTUP_PROMETHEUS_URL is required")

    kubeconfig = kubernetes_access_from_env()
    deployment_namespace = os.environ.get("AXERN_STARTUP_DEPLOYMENT_NAMESPACE", "axern-system")

    emit("soak_prepare", warmup_cohorts=warmup_cohorts, cohort_size=cohort_size)
    wait_for_idle(prometheus_url)
    for warmup in range(1, warmup_cohorts + 1):
        for scenario_index, scenario in enumerate(scenarios, start=1):
            output = output_dir / f"warmup-{warmup:05d}-{scenario_index:02d}.jsonl"
            emit("warmup_start", warmup=warmup, scenario=scenario, output=str(output))
            returncode = run_cohort(output, cohort_size, scenario)
            emit("warmup_end", warmup=warmup, scenario=scenario, returncode=returncode)
            if returncode != 0:
                raise SystemExit(1)
            wait_for_idle(prometheus_url)

    wait_for_idle(prometheus_url)
    write_snapshot(output_dir / "metrics-before.json", prometheus_url, kubeconfig, deployment_namespace)
    started = time.monotonic()
    emit("soak_start", duration_seconds=duration_seconds, cohort_size=cohort_size)

    cohort = 0
    failures = 0
    while time.monotonic() - started < duration_seconds:
        if _STOP_REQUESTED:
            break
        cohort += 1
        cohort_started = time.monotonic()
        returncode = 0
        for scenario_index, scenario in enumerate(scenarios, start=1):
            output = output_dir / f"cohort-{cohort:05d}-{scenario_index:02d}.jsonl"
            emit("cohort_wave_start", cohort=cohort, scenario=scenario, output=str(output))
            returncode = run_cohort(output, cohort_size, scenario)
            emit("cohort_wave_end", cohort=cohort, scenario=scenario, returncode=returncode)
            if returncode != 0:
                failures += 1
                break
            wait_for_idle(prometheus_url)
        emit(
            "cohort_end",
            cohort=cohort,
            elapsed_seconds=round(time.monotonic() - cohort_started, 3),
            returncode=returncode,
        )
        if returncode != 0:
            break

    wait_for_idle(prometheus_url)
    write_snapshot(output_dir / "metrics-after.json", prometheus_url, kubeconfig, deployment_namespace)
    elapsed = time.monotonic() - started
    emit(
        "soak_end",
        cohorts=cohort,
        elapsed_seconds=round(elapsed, 3),
        failures=failures,
        completed_duration=elapsed >= duration_seconds,
    )
    if failures or elapsed < duration_seconds or _STOP_REQUESTED:
        raise SystemExit(1)


def run_cohort(output: Path, cohort_size: int, scenario: str) -> int:
    env = os.environ.copy()
    env.update(
        {
            "AXERN_STARTUP_PHASES": "service-fanout",
            "AXERN_STARTUP_STAGES": str(cohort_size),
            "AXERN_STARTUP_ITERATIONS": "1",
            "AXERN_STARTUP_STAGE_PAUSE_SECONDS": "0",
            "AXERN_STARTUP_SCENARIOS": scenario,
        }
    )
    for key in (
        "HTTP_PROXY",
        "HTTPS_PROXY",
        "ALL_PROXY",
        "http_proxy",
        "https_proxy",
        "all_proxy",
    ):
        env.pop(key, None)
    env["NO_PROXY"] = "*"
    env["no_proxy"] = "*"
    return run_program(output, Path(__file__).with_name("readiness.py"), env)


def run_program(output: Path, program: Path, env: dict[str, str]) -> int:
    global _ACTIVE_RUN
    with output.open("w", encoding="utf-8") as stream:
        process = subprocess.Popen(
            [sys.executable, str(program)],
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            bufsize=1,
        )
        _ACTIVE_RUN = process
        assert process.stdout is not None
        try:
            for line in process.stdout:
                stream.write(line)
                stream.flush()
                print(line, end="", flush=True)
            return process.wait()
        finally:
            _ACTIVE_RUN = None


def install_signal_handlers() -> None:
    signal.signal(signal.SIGTERM, request_stop)
    signal.signal(signal.SIGINT, request_stop)


def request_stop(signum: int, _frame: Any) -> None:
    global _STOP_REQUESTED
    _STOP_REQUESTED = True
    process = _ACTIVE_RUN
    if process is not None and process.poll() is None:
        process.send_signal(signal.SIGINT)
    emit("soak_stop_requested", signal=signal.Signals(signum).name)


def stop_requested() -> bool:
    return _STOP_REQUESTED


def write_snapshot(path: Path, prometheus_url: str, kubeconfig: Path | None, namespace: str) -> None:
    captured: dict[str, Any] = {"captured_at": time.time(), "queries": {}, "errors": {}}
    for name, query in DEFAULT_QUERIES.items():
        try:
            captured["queries"][name] = prometheus_query(prometheus_url, query)
        except Exception as exc:
            captured["errors"][name] = f"{type(exc).__name__}: {exc}"
    try:
        captured["pod_resources"] = kubernetes_pod_metrics(kubeconfig, namespace)
    except Exception as exc:
        captured["errors"]["pod_resources"] = f"{type(exc).__name__}: {exc}"
    path.write_text(json.dumps(captured, indent=2, sort_keys=True) + "\n")
    if captured["errors"]:
        raise RuntimeError(f"resource snapshot failed: {captured['errors']}")


def kubernetes_pod_metrics(kubeconfig: Path | None, namespace: str) -> dict[str, Any]:
    if kubeconfig is None:
        return in_cluster_pod_metrics(namespace)

    env = no_proxy_env()

    result = subprocess.run(
        [
            "kubectl",
            "--kubeconfig",
            str(kubeconfig),
            "get",
            "--raw",
            f"/apis/metrics.k8s.io/v1beta1/namespaces/{namespace}/pods",
        ],
        env=env,
        check=True,
        capture_output=True,
        text=True,
    )
    payload = json.loads(result.stdout)
    if not payload.get("items"):
        raise RuntimeError("Kubernetes pod metrics are empty")
    return payload


def in_cluster_pod_metrics(namespace: str) -> dict[str, Any]:
    host = required_env("KUBERNETES_SERVICE_HOST")
    port = os.environ.get("KUBERNETES_SERVICE_PORT_HTTPS", "443").strip() or "443"
    token_path = Path("/var/run/secrets/kubernetes.io/serviceaccount/token")
    ca_path = Path("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
    if not token_path.is_file() or not ca_path.is_file():
        raise RuntimeError("Kubernetes ServiceAccount credentials are unavailable")
    if ":" in host and not host.startswith("["):
        host = f"[{host}]"
    namespace_path = urllib.parse.quote(namespace, safe="")
    request = urllib.request.Request(
        f"https://{host}:{port}/apis/metrics.k8s.io/v1beta1/namespaces/{namespace_path}/pods",
        headers={"Authorization": f"Bearer {token_path.read_text().strip()}"},
    )
    opener = urllib.request.build_opener(
        urllib.request.ProxyHandler({}),
        urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=str(ca_path))),
    )
    with opener.open(request, timeout=10.0) as response:
        payload = json.load(response)
    if not payload.get("items"):
        raise RuntimeError("Kubernetes pod metrics are empty")
    return payload


def kubernetes_access_from_env() -> Path | None:
    if boolean_env("AXERN_SOAK_IN_CLUSTER", False):
        if os.environ.get("AXERN_SOAK_KUBECONFIG", "").strip() or os.environ.get(
            "AXERN_SOAK_EXPECTED_CONTEXT", ""
        ).strip():
            raise SystemExit("in-cluster soak must not configure a kubeconfig or expected context")
        return None

    kubeconfig = Path(required_env("AXERN_SOAK_KUBECONFIG"))
    if not kubeconfig.is_file():
        raise SystemExit(f"AXERN_SOAK_KUBECONFIG does not exist: {kubeconfig}")
    verify_kube_context(kubeconfig, required_env("AXERN_SOAK_EXPECTED_CONTEXT"))
    return kubeconfig


def verify_kube_context(kubeconfig: Path, expected: str) -> None:
    result = subprocess.run(
        ["kubectl", "--kubeconfig", str(kubeconfig), "config", "current-context"],
        env=no_proxy_env(),
        check=True,
        capture_output=True,
        text=True,
    )
    actual = result.stdout.strip()
    if actual != expected:
        raise SystemExit(f"kube context {actual!r} does not match expected {expected!r}")


def no_proxy_env() -> dict[str, str]:
    env = os.environ.copy()
    for key in ("HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY", "http_proxy", "https_proxy", "all_proxy"):
        env.pop(key, None)
    env["NO_PROXY"] = "*"
    env["no_proxy"] = "*"
    return env


def wait_for_idle(prometheus_url: str) -> None:
    deadline = time.monotonic() + positive_float("AXERN_SOAK_IDLE_TIMEOUT_SECONDS", 300.0)
    while time.monotonic() < deadline:
        queue = prometheus_query(prometheus_url, DEFAULT_QUERIES["allocation_queue"])
        allocations = prometheus_query(prometheus_url, DEFAULT_QUERIES["active_allocations"])
        if (
            queue
            and allocations
            and float(queue[0]["value"][1]) == 0
            and float(allocations[0]["value"][1]) == 0
        ):
            return
        time.sleep(1.0)
    raise RuntimeError("Axern did not become idle before the soak deadline")


def prometheus_query(base_url: str, query: str) -> list[dict[str, Any]]:
    url = f"{base_url}/api/v1/query?{urllib.parse.urlencode({'query': query})}"
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(url, timeout=10.0) as response:
        payload = json.load(response)
    if payload.get("status") != "success":
        raise RuntimeError(payload.get("error", "Prometheus query failed"))
    return payload.get("data", {}).get("result", [])


def positive_float(name: str, default: float) -> float:
    value = float(os.environ.get(name, str(default)))
    if not math.isfinite(value) or value <= 0:
        raise SystemExit(f"{name} must be positive")
    return value


def positive_int(name: str, default: int) -> int:
    value = int(os.environ.get(name, str(default)))
    if value <= 0:
        raise SystemExit(f"{name} must be positive")
    return value


def nonnegative_int(name: str, default: int) -> int:
    value = int(os.environ.get(name, str(default)))
    if value < 0:
        raise SystemExit(f"{name} must be nonnegative")
    return value


def nonnegative_float(name: str, default: float) -> float:
    value = float(os.environ.get(name, str(default)))
    if not math.isfinite(value) or value < 0:
        raise SystemExit(f"{name} must be nonnegative")
    return value


def boolean_env(name: str, default: bool) -> bool:
    raw = os.environ.get(name)
    if raw is None:
        return default
    value = raw.strip().lower()
    if value in {"1", "true", "yes", "on"}:
        return True
    if value in {"0", "false", "no", "off"}:
        return False
    raise SystemExit(f"{name} must be a boolean")


def required_env(name: str) -> str:
    value = os.environ.get(name, "").strip()
    if not value:
        raise SystemExit(f"missing required env: {name}")
    return value


def required_list_env(name: str) -> tuple[str, ...]:
    values = tuple(value.strip() for value in required_env(name).split(",") if value.strip())
    if not values:
        raise SystemExit(f"{name} must contain at least one value")
    if len(values) != len(set(values)):
        raise SystemExit(f"{name} must not contain duplicate values")
    return values


def emit(event: str, **fields: Any) -> None:
    print(json.dumps({"event": event, "ts": time.time_ns(), **fields}, sort_keys=True), flush=True)


if __name__ == "__main__":
    main()
