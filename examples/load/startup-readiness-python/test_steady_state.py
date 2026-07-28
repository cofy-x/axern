from __future__ import annotations

import asyncio
import importlib.util
import io
import json
from pathlib import Path
import sys
from types import SimpleNamespace
import unittest
from unittest import mock


MODULE_PATH = Path(__file__).with_name("steady_state.py")
SPEC = importlib.util.spec_from_file_location("steady_state", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
sys.path.insert(0, str(MODULE_PATH.parent))
steady = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = steady
SPEC.loader.exec_module(steady)


class SteadyStateTest(unittest.TestCase):
    def test_rates_must_be_positive_and_finite(self) -> None:
        self.assertEqual(steady.parse_rates("1,2.5"), (1.0, 2.5))
        for value in ("", "0", "nan", "1,inf"):
            with self.subTest(value=value), self.assertRaises(SystemExit):
                steady.parse_rates(value)

    def test_summary_preserves_scenario_and_error_breakdown(self) -> None:
        results = [
            steady.LifecycleResult(
                ok=True,
                scenario="oci",
                arrival_rate=2.0,
                index=0,
                schedule_lag_seconds=0.01,
                create_ack_seconds=0.1,
                ready_seconds=0.8,
                first_http_seconds=0.9,
                node_id="node-a",
            ),
            steady.LifecycleResult(
                ok=False,
                scenario="nydus",
                arrival_rate=2.0,
                index=1,
                schedule_lag_seconds=0.02,
                error_stage="ready",
                error="failed",
            ),
        ]

        summary = steady.SteadyAccumulator()
        for result in results:
            summary.record(result)

        with mock.patch("sys.stdout", new_callable=io.StringIO) as output:
            steady.emit_summary(2.0, 10.0, 3.0, summary)

        event = json.loads(output.getvalue())
        self.assertEqual(event["ok"], 1)
        self.assertEqual(event["failed"], 1)
        self.assertEqual(event["error_stages"], {"ready": 1})
        self.assertEqual(event["scenario_counts"], {"nydus": 1, "oci": 1})


class SteadyStateAsyncTest(unittest.IsolatedAsyncioTestCase):
    async def test_cancellation_drains_lifecycles_before_environment_cleanup(self) -> None:
        events = []
        lifecycle_started = asyncio.Event()
        client = mock.Mock()
        client.create_environment = mock.AsyncMock(return_value=SimpleNamespace(id="env-a"))

        async def delete_environment(*_args, **_kwargs):
            events.append("environment_cleanup")

        async def run_lifecycle(*_args, **_kwargs):
            lifecycle_started.set()
            try:
                await asyncio.Event().wait()
            finally:
                events.append("service_cleanup")

        client.delete_environment = mock.AsyncMock(side_effect=delete_environment)
        config = SimpleNamespace(
            scenarios=(SimpleNamespace(name="oci", image_ref="registry/image:tag"),),
            namespace="default",
            registry_credential_id="secret-a",
        )
        with (
            mock.patch.object(steady, "run_lifecycle", side_effect=run_lifecycle),
            mock.patch.object(steady.readiness, "emit"),
        ):
            run = asyncio.create_task(steady.run_rate(client, config, 1.0, 60.0, 30.0, 2, 0.25))
            await lifecycle_started.wait()
            run.cancel()
            with self.assertRaises(asyncio.CancelledError):
                await run

        self.assertEqual(events, ["service_cleanup", "environment_cleanup"])

    async def test_environment_cleanup_failure_is_returned(self) -> None:
        client = mock.Mock()
        client.create_environment = mock.AsyncMock(return_value=SimpleNamespace(id="env-a"))
        client.delete_environment = mock.AsyncMock(side_effect=RuntimeError("delete failed"))
        config = SimpleNamespace(
            scenarios=(SimpleNamespace(name="oci", image_ref="registry/image:tag"),),
            namespace="default",
            registry_credential_id="secret-a",
        )
        lifecycle = steady.LifecycleResult(
            ok=True,
            scenario="oci",
            arrival_rate=1.0,
            index=0,
            schedule_lag_seconds=0.0,
        )

        with (
            mock.patch.object(steady, "run_lifecycle", new=mock.AsyncMock(return_value=lifecycle)),
            mock.patch.object(steady.readiness, "emit"),
        ):
            summary = await steady.run_rate(client, config, 1.0, 0.001, 0.001, 2, 0.25)

        self.assertEqual(summary.scheduled, 2)
        self.assertEqual(summary.ok, 1)
        self.assertEqual(summary.failed, 1)
        self.assertEqual(summary.error_stages, {"environment_cleanup": 1})


if __name__ == "__main__":
    unittest.main()
