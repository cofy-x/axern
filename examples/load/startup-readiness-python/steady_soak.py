from __future__ import annotations

import os
from pathlib import Path
import time

import lifecycle_soak


def main() -> None:
    lifecycle_soak.install_signal_handlers()
    duration = lifecycle_soak.positive_float("AXERN_SOAK_DURATION_SECONDS", 21600.0)
    canary_duration = lifecycle_soak.nonnegative_float("AXERN_SOAK_CANARY_DURATION_SECONDS", 90.0)
    arrival_rate = lifecycle_soak.positive_float("AXERN_SOAK_ARRIVAL_RATE", 12.0)
    lifetime = lifecycle_soak.positive_float("AXERN_SOAK_SERVICE_LIFETIME_SECONDS", 30.0)
    output_dir = Path(os.environ.get("AXERN_SOAK_OUTPUT_DIR", "work/axern-steady-soak"))
    output_dir.mkdir(parents=True, exist_ok=True)

    prometheus_url = lifecycle_soak.required_env("AXERN_STARTUP_PROMETHEUS_URL").rstrip("/")
    kubeconfig = lifecycle_soak.kubernetes_access_from_env()
    namespace = os.environ.get("AXERN_STARTUP_DEPLOYMENT_NAMESPACE", "axern-system")
    lifecycle_soak.wait_for_idle(prometheus_url)

    if canary_duration > 0:
        lifecycle_soak.emit(
            "steady_canary_start",
            arrival_rate=arrival_rate,
            duration_seconds=canary_duration,
            lifetime_seconds=lifetime,
        )
        if run_steady(output_dir / "canary.jsonl", arrival_rate, canary_duration, lifetime) != 0:
            raise SystemExit(1)
        lifecycle_soak.wait_for_idle(prometheus_url)
        lifecycle_soak.emit("steady_canary_passed")

    lifecycle_soak.write_snapshot(
        output_dir / "metrics-before.json", prometheus_url, kubeconfig, namespace
    )
    lifecycle_soak.emit(
        "steady_soak_start",
        arrival_rate=arrival_rate,
        duration_seconds=duration,
        lifetime_seconds=lifetime,
    )
    started = time.monotonic()
    returncode = run_steady(output_dir / "steady.jsonl", arrival_rate, duration, lifetime)
    lifecycle_soak.wait_for_idle(prometheus_url)
    lifecycle_soak.write_snapshot(
        output_dir / "metrics-after.json", prometheus_url, kubeconfig, namespace
    )
    lifecycle_soak.emit(
        "steady_soak_end",
        elapsed_seconds=round(time.monotonic() - started, 3),
        returncode=returncode,
    )
    if returncode != 0 or lifecycle_soak.stop_requested():
        raise SystemExit(1)


def run_steady(output: Path, arrival_rate: float, duration: float, lifetime: float) -> int:
    env = lifecycle_soak.no_proxy_env()
    env.update(
        {
            "AXERN_STEADY_ARRIVAL_RATES": str(arrival_rate),
            "AXERN_STEADY_DURATION_SECONDS": str(duration),
            "AXERN_STEADY_SERVICE_LIFETIME_SECONDS": str(lifetime),
        }
    )
    return lifecycle_soak.run_program(output, Path(__file__).with_name("steady_state.py"), env)


if __name__ == "__main__":
    main()
