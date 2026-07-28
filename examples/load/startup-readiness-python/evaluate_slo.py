from __future__ import annotations

import argparse
import json
from pathlib import Path
from typing import Any


def main() -> None:
    parser = argparse.ArgumentParser(description="Evaluate startup-readiness JSONL against an SLO policy")
    parser.add_argument("--policy", type=Path, required=True)
    parser.add_argument("--input", type=Path, action="append", required=True)
    args = parser.parse_args()

    policy = json.loads(args.policy.read_text())
    if policy.get("schema_version") != 1:
        raise SystemExit("SLO policy must use schema_version 1")

    failures = evaluate_inputs(args.input, policy)
    print(json.dumps({"inputs": len(args.input), "failures": failures}, sort_keys=True))
    if failures:
        raise SystemExit(1)


def evaluate_inputs(paths: list[Path], policy: dict[str, Any]) -> list[dict[str, Any]]:
    summaries: dict[tuple[str, str, int, str], list[tuple[Path, dict[str, Any]]]] = {}
    failures: list[dict[str, Any]] = []
    for path in paths:
        events = [json.loads(line) for line in path.read_text().splitlines() if line.strip()]
        for event in events:
            if event.get("event") != "summary" or event.get("scope") != "final":
                continue
            key = summary_key(event)
            summaries.setdefault(key, []).append((path, event))
        if policy.get("reject_failure_events", True):
            failure_events = sum(event.get("event") == "failure" for event in events)
            if failure_events:
                failures.append(failure(path, (), "failure_events", actual=failure_events, limit=0))
        if policy.get("require_metrics_complete", True):
            metrics = [event for event in events if event.get("event") == "metrics_summary"]
            if not metrics or any(event.get("complete") is not True for event in metrics):
                failures.append(failure(path, (), "metrics_incomplete"))

    for requirement in policy.get("requirements", []):
        key = requirement_key(requirement)
        observations = summaries.get(key)
        if not observations:
            failures.append(failure(Path(""), key, "missing_summary"))
            continue
        for path, summary in observations:
            failures.extend(evaluate_summary(path, key, summary, requirement))
        max_share_deviation = requirement.get("max_aggregate_node_share_deviation")
        if max_share_deviation is not None:
            counts: dict[str, int] = {}
            for _, summary in observations:
                for node, count in summary.get("node_counts", {}).items():
                    counts[node] = counts.get(node, 0) + int(count)
            total = sum(counts.values())
            deviation = None
            if counts and total > 0:
                expected_share = 1.0 / len(counts)
                deviation = max(abs(count / total - expected_share) for count in counts.values())
            if deviation is None or deviation > max_share_deviation:
                failures.append(
                    failure(
                        Path(""),
                        key,
                        "aggregate_node_share_deviation",
                        actual=deviation,
                        limit=max_share_deviation,
                    )
                )
    return failures


def evaluate_summary(
    path: Path,
    key: tuple[str, str, int, str],
    summary: dict[str, Any],
    requirement: dict[str, Any],
) -> list[dict[str, Any]]:
    failures = []
    checks = (
        ("ok", summary.get("ok", 0), requirement.get("min_ok"), lambda actual, limit: actual >= limit),
        ("failed", summary.get("failed", 0), requirement.get("max_failed"), lambda actual, limit: actual <= limit),
        (
            "p95_seconds",
            summary.get("latency_seconds", {}).get("p95"),
            requirement.get("max_p95_seconds"),
            lambda actual, limit: actual is not None and actual <= limit,
        ),
    )
    for name, actual, limit, predicate in checks:
        if limit is not None and not predicate(actual, limit):
            failures.append(failure(path, key, name, actual=actual, limit=limit))

    max_node_skew = requirement.get("max_node_skew")
    if max_node_skew is not None:
        counts = list(summary.get("node_counts", {}).values())
        skew = max(counts) - min(counts) if counts else None
        if skew is None or skew > max_node_skew:
            failures.append(failure(path, key, "node_skew", actual=skew, limit=max_node_skew))
    return failures


def summary_key(event: dict[str, Any]) -> tuple[str, str, int, str]:
    return (
        str(event.get("topology", "")),
        str(event.get("scenario", "")),
        int(event.get("stage", 0)),
        str(event.get("phase", "")),
    )


def requirement_key(requirement: dict[str, Any]) -> tuple[str, str, int, str]:
    return (
        str(requirement["topology"]),
        str(requirement["scenario"]),
        int(requirement["stage"]),
        str(requirement["phase"]),
    )


def failure(
    path: Path,
    key: tuple[Any, ...],
    check: str,
    *,
    actual: Any = None,
    limit: Any = None,
) -> dict[str, Any]:
    return {"input": str(path), "key": key, "check": check, "actual": actual, "limit": limit}


if __name__ == "__main__":
    main()
