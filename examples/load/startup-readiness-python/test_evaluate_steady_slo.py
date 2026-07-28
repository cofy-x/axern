from __future__ import annotations

import importlib.util
import json
from pathlib import Path
import sys
import tempfile
import unittest


MODULE_PATH = Path(__file__).with_name("evaluate_steady_slo.py")
SPEC = importlib.util.spec_from_file_location("evaluate_steady_slo", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
slo = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = slo
SPEC.loader.exec_module(slo)


class EvaluateSteadySLOTest(unittest.TestCase):
    def test_accepts_complete_balanced_result(self) -> None:
        self.assertEqual(self.evaluate(summary()), [])

    def test_reports_latency_failure_and_accounting(self) -> None:
        event = summary()
        event["ok"] = 70
        event["failed"] = 1
        event["ready_seconds"]["p95"] = 3.0
        checks = {item["check"] for item in self.evaluate(event)}
        self.assertEqual(checks, {"result_accounting", "failed", "success_ratio", "ready_seconds.p95"})

    def test_rejects_missing_runtime_node_even_when_remaining_nodes_are_balanced(self) -> None:
        event = summary()
        event["scheduled"] = 70
        event["ok"] = 70
        event["node_counts"] = {f"node-{index}": 14 for index in range(5)}

        checks = {item["check"] for item in self.evaluate(event)}

        self.assertEqual(checks, {"node_count"})

    def evaluate(self, event: dict) -> list[dict]:
        policy = {
            "arrival_rate": 12,
            "max_failed": 0,
            "min_success_ratio": 1.0,
            "expected_node_count": 6,
            "max_node_share_deviation": 0.02,
            "max_p95_seconds": {"ready_seconds": 2.0},
        }
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "steady.jsonl"
            path.write_text(json.dumps(event) + "\n")
            return slo.evaluate(path, policy)


def summary() -> dict:
    return {
        "event": "steady_summary",
        "arrival_rate": 12,
        "scheduled": 72,
        "ok": 72,
        "failed": 0,
        "node_counts": {f"node-{index}": 12 for index in range(6)},
        "ready_seconds": {"p95": 0.7},
    }


if __name__ == "__main__":
    unittest.main()
