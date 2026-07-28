from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


def main() -> None:
    parser = argparse.ArgumentParser(description="Evaluate a steady-state JSONL result against an SLO")
    parser.add_argument("--policy", type=Path, required=True)
    parser.add_argument("--input", type=Path, required=True)
    args = parser.parse_args()

    policy = json.loads(args.policy.read_text())
    if policy.get("schema_version") != 1:
        raise SystemExit("steady SLO policy must use schema_version 1")
    failures = evaluate(args.input, policy)
    print(json.dumps({"input": str(args.input), "failures": failures}, sort_keys=True))
    if failures:
        raise SystemExit(1)


def evaluate(path: Path, policy: dict[str, Any]) -> list[dict[str, Any]]:
    summaries: list[dict[str, Any]] = []
    failure_events = 0
    with path.open(encoding="utf-8") as stream:
        for line in stream:
            if not line.strip():
                continue
            event = json.loads(line)
            if event.get("event") == "steady_summary":
                summaries.append(event)
            elif event.get("event") == "failure":
                failure_events += 1

    failures: list[dict[str, Any]] = []
    if len(summaries) != 1:
        return [failure("summary_count", len(summaries), 1)]
    if policy.get("reject_failure_events", True) and failure_events:
        failures.append(failure("failure_events", failure_events, 0))

    summary = summaries[0]
    expected_rate = float(policy["arrival_rate"])
    if float(summary.get("arrival_rate", 0)) != expected_rate:
        failures.append(failure("arrival_rate", summary.get("arrival_rate"), expected_rate))
    scheduled = int(summary.get("scheduled", 0))
    ok = int(summary.get("ok", 0))
    failed = int(summary.get("failed", 0))
    if scheduled <= 0 or ok + failed != scheduled:
        failures.append(failure("result_accounting", ok + failed, scheduled))
    max_failed = int(policy.get("max_failed", 0))
    if failed > max_failed:
        failures.append(failure("failed", failed, max_failed))
    ratio = ok / scheduled if scheduled else 0.0
    min_ratio = float(policy.get("min_success_ratio", 1.0))
    if ratio < min_ratio:
        failures.append(failure("success_ratio", ratio, min_ratio))

    for metric, limit in policy.get("max_p95_seconds", {}).items():
        actual = summary.get(metric, {}).get("p95")
        if actual is None or float(actual) > float(limit):
            failures.append(failure(f"{metric}.p95", actual, limit))
    max_deviation = policy.get("max_node_share_deviation")
    if max_deviation is not None:
        counts = [int(value) for value in summary.get("node_counts", {}).values()]
        expected_node_count = policy.get("expected_node_count")
        if expected_node_count is not None and len(counts) != int(expected_node_count):
            failures.append(failure("node_count", len(counts), int(expected_node_count)))
        total = sum(counts)
        deviation = None
        if counts and total:
            expected = 1.0 / len(counts)
            deviation = max(abs(count / total - expected) for count in counts)
        if deviation is None or deviation > float(max_deviation):
            failures.append(failure("node_share_deviation", deviation, max_deviation))
    return failures


def failure(check: str, actual: Any, limit: Any) -> dict[str, Any]:
    return {"check": check, "actual": actual, "limit": limit}


if __name__ == "__main__":
    main()
