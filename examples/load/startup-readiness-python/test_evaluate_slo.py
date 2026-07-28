from __future__ import annotations

import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest


MODULE_PATH = Path(__file__).with_name("evaluate_slo.py")
SPEC = importlib.util.spec_from_file_location("evaluate_slo", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
slo = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = slo
SPEC.loader.exec_module(slo)


class EvaluateSLOTest(unittest.TestCase):
    def test_combines_stage_summaries_from_multiple_inputs(self) -> None:
        policy = {
            "requirements": [requirement(36), requirement(72)],
            "require_metrics_complete": True,
        }
        with tempfile.TemporaryDirectory() as directory:
            paths = []
            for stage in (36, 72):
                path = Path(directory) / f"stage-{stage}.jsonl"
                write_events(path, summary(stage), {"event": "metrics_summary", "complete": True})
                paths.append(path)

            failures = slo.evaluate_inputs(paths, policy)

        self.assertEqual(failures, [])

    def test_reports_latency_and_node_skew_regressions(self) -> None:
        policy = {"requirements": [requirement(36)], "require_metrics_complete": True}
        event = summary(36)
        event["latency_seconds"]["p95"] = 3.0
        event["node_counts"] = {"node-a": 8, "node-b": 4}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "result.jsonl"
            write_events(path, event, {"event": "metrics_summary", "complete": True})

            failures = slo.evaluate_inputs([path], policy)

        self.assertEqual({item["check"] for item in failures}, {"p95_seconds", "node_skew"})

    def test_repeated_soak_observations_are_each_evaluated(self) -> None:
        item = requirement(36)
        item.pop("max_node_skew")
        item["max_aggregate_node_share_deviation"] = 0.01
        policy = {"requirements": [item], "require_metrics_complete": True}
        with tempfile.TemporaryDirectory() as directory:
            paths = []
            for index in range(2):
                path = Path(directory) / f"cohort-{index}.jsonl"
                write_events(path, summary(36), {"event": "metrics_summary", "complete": True})
                paths.append(path)

            failures = slo.evaluate_inputs(paths, policy)

        self.assertEqual(failures, [])

    def test_failure_event_fails_even_when_required_summary_passes(self) -> None:
        policy = {"requirements": [requirement(36)], "require_metrics_complete": True}
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "result.jsonl"
            write_events(
                path,
                summary(36),
                {"event": "metrics_summary", "complete": True},
                {"event": "failure", "phase": "service_cleanup", "error": "purge failed"},
            )

            failures = slo.evaluate_inputs([path], policy)

        self.assertEqual([item["check"] for item in failures], ["failure_events"])


def requirement(stage: int) -> dict:
    return {
        "topology": "service-fanout",
        "scenario": "tiny-go-http",
        "stage": stage,
        "phase": "service_replica_ready",
        "min_ok": stage,
        "max_failed": 0,
        "max_p95_seconds": 2.0,
        "max_node_skew": 1,
    }


def summary(stage: int) -> dict:
    return {
        "event": "summary",
        "scope": "final",
        "topology": "service-fanout",
        "scenario": "tiny-go-http",
        "stage": stage,
        "phase": "service_replica_ready",
        "ok": stage,
        "failed": 0,
        "latency_seconds": {"p95": 1.0},
        "node_counts": {"node-a": stage // 2, "node-b": stage // 2},
    }


def write_events(path: Path, *events: dict) -> None:
    path.write_text("".join(json.dumps(event) + "\n" for event in events))


if __name__ == "__main__":
    unittest.main()
