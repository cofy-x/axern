from __future__ import annotations

import importlib.util
from pathlib import Path
import sys
import time
import unittest

from axern.control.common.v1 import common_pb2
from axern.control.service.v1 import service_replica_pb2, service_types_pb2


MODULE_PATH = Path(__file__).with_name("capacity_probe.py")
SPEC = importlib.util.spec_from_file_location("capacity_probe", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
capacity = importlib.util.module_from_spec(SPEC)
sys.path.insert(0, str(MODULE_PATH.parent))
sys.modules[SPEC.name] = capacity
SPEC.loader.exec_module(capacity)


class FakeClient:
    def list_service_replicas(self, service_id: str, timeout: float):
        del timeout
        return [
            service_replica_pb2.ServiceReplica(
                id=f"alloc-{service_id}",
                node_id="node-a",
                status=common_pb2.ALLOCATION_STATUS_RUNNING,
                ready=True,
            )
        ]


class CapacityProbeTest(unittest.TestCase):
    def test_ready_uses_batch_service_projection(self) -> None:
        outcome = capacity.classify_service(
            service_types_pb2.Service(
                id="svc-a",
                status=service_types_pb2.SERVICE_STATUS_READY,
                ready_replicas=1,
            ),
            time.monotonic(),
        )

        self.assertIsNotNone(outcome)
        assert outcome is not None
        self.assertEqual(outcome.outcome, "ready")
        self.assertEqual(outcome.node_id, "")

    def test_admission_blocked_uses_structured_diagnostic(self) -> None:
        outcome = capacity.classify_service(
            service_types_pb2.Service(
                id="svc-b",
                status=service_types_pb2.SERVICE_STATUS_DEGRADED,
                diagnostic_code=common_pb2.WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED,
                message="details may change",
            ),
            time.monotonic(),
        )

        self.assertIsNotNone(outcome)
        assert outcome is not None
        self.assertEqual(outcome.outcome, "admission_blocked")
        self.assertEqual(outcome.diagnostic_code, "WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED")

    def test_other_degraded_service_is_unexpected(self) -> None:
        outcome = capacity.classify_service(
            service_types_pb2.Service(
                id="svc-c",
                status=service_types_pb2.SERVICE_STATUS_DEGRADED,
                diagnostic_code=common_pb2.WORKLOAD_DIAGNOSTIC_CODE_RUNTIME_START_ERROR,
            ),
            time.monotonic(),
        )

        self.assertIsNotNone(outcome)
        assert outcome is not None
        self.assertEqual(outcome.outcome, "unexpected")

    def test_node_lookup_is_post_measurement_and_parallelizable(self) -> None:
        outcomes = [
            capacity.CapacityOutcome(
                service_id="svc-a",
                outcome="ready",
                elapsed_seconds=0.5,
            )
        ]

        attached = capacity.attach_ready_nodes(FakeClient(), outcomes)

        self.assertEqual(attached[0].elapsed_seconds, 0.5)
        self.assertEqual(attached[0].node_id, "node-a")


if __name__ == "__main__":
    unittest.main()
