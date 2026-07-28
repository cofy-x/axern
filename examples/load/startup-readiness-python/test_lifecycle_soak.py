from __future__ import annotations

import importlib.util
import io
import os
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock


MODULE_PATH = Path(__file__).with_name("lifecycle_soak.py")
SPEC = importlib.util.spec_from_file_location("lifecycle_soak", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
soak = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = soak
SPEC.loader.exec_module(soak)


class LifecycleSoakTest(unittest.TestCase):
    def test_run_cohort_sets_strict_lifecycle_and_disables_proxy(self) -> None:
        captured = {}

        process = mock.Mock()
        process.stdout = iter(['{"event":"summary"}\n'])
        process.wait.return_value = 0

        def fake_popen(*args, **kwargs):
            captured.update(kwargs)
            return process

        with tempfile.TemporaryDirectory() as directory, mock.patch.dict(
            os.environ,
            {"HTTP_PROXY": "http://proxy", "AXERN_STARTUP_PHASES": "sandbox"},
            clear=False,
        ), mock.patch.object(soak.subprocess, "Popen", side_effect=fake_popen), mock.patch(
            "sys.stdout", new_callable=io.StringIO
        ):
            result = soak.run_cohort(Path(directory) / "cohort.jsonl", 12, "tiny")

        self.assertEqual(result, 0)
        self.assertEqual(captured["env"]["AXERN_STARTUP_PHASES"], "service-fanout")
        self.assertEqual(captured["env"]["AXERN_STARTUP_STAGES"], "12")
        self.assertEqual(captured["env"]["AXERN_STARTUP_SCENARIOS"], "tiny")
        self.assertNotIn("HTTP_PROXY", captured["env"])
        self.assertEqual(captured["env"]["NO_PROXY"], "*")

    def test_positive_values_reject_zero(self) -> None:
        with mock.patch.dict(os.environ, {"VALUE": "0"}):
            with self.assertRaises(SystemExit):
                soak.positive_float("VALUE", 1.0)
            with self.assertRaises(SystemExit):
                soak.positive_int("VALUE", 1)

    def test_nonnegative_value_accepts_zero(self) -> None:
        with mock.patch.dict(os.environ, {"VALUE": "0"}):
            self.assertEqual(soak.nonnegative_int("VALUE", 1), 0)

    def test_positive_float_rejects_non_finite_values(self) -> None:
        for value in ("nan", "inf", "-inf"):
            with self.subTest(value=value), mock.patch.dict(os.environ, {"VALUE": value}):
                with self.assertRaises(SystemExit):
                    soak.positive_float("VALUE", 1.0)

    def test_kube_context_must_match_explicit_target(self) -> None:
        completed = mock.Mock(stdout="unexpected\n")
        with mock.patch.object(soak.subprocess, "run", return_value=completed):
            with self.assertRaisesRegex(SystemExit, "does not match expected"):
                soak.verify_kube_context(Path("kubeconfig"), "expected")

    def test_snapshot_fails_when_any_query_fails(self) -> None:
        with tempfile.TemporaryDirectory() as directory, mock.patch.object(
            soak, "prometheus_query", side_effect=RuntimeError("unavailable")
        ):
            with self.assertRaises(RuntimeError):
                soak.write_snapshot(
                    Path(directory) / "snapshot.json",
                    "http://prometheus",
                    Path("kubeconfig"),
                    "axern-system",
                )

    def test_in_cluster_access_rejects_ambiguous_kubeconfig(self) -> None:
        with mock.patch.dict(
            os.environ,
            {"AXERN_SOAK_IN_CLUSTER": "true", "AXERN_SOAK_KUBECONFIG": "/tmp/config"},
            clear=True,
        ):
            with self.assertRaisesRegex(SystemExit, "must not configure a kubeconfig"):
                soak.kubernetes_access_from_env()

    def test_in_cluster_access_uses_service_account(self) -> None:
        with mock.patch.dict(os.environ, {"AXERN_SOAK_IN_CLUSTER": "true"}, clear=True):
            self.assertIsNone(soak.kubernetes_access_from_env())

    def test_boolean_env_rejects_unknown_value(self) -> None:
        with mock.patch.dict(os.environ, {"VALUE": "sometimes"}):
            with self.assertRaisesRegex(SystemExit, "must be a boolean"):
                soak.boolean_env("VALUE", False)

    def test_required_list_rejects_duplicates(self) -> None:
        with mock.patch.dict(os.environ, {"VALUE": "oci,nydus,oci"}):
            with self.assertRaisesRegex(SystemExit, "duplicate"):
                soak.required_list_env("VALUE")

    def test_idle_wait_requires_queue_and_active_allocations_to_reach_zero(self) -> None:
        samples = iter((0, 1, 0, 0))

        def query(_url: str, _query: str):
            return [{"value": [0, str(next(samples))]}]

        with mock.patch.object(soak, "prometheus_query", side_effect=query), mock.patch.object(
            soak.time, "sleep"
        ):
            soak.wait_for_idle("http://prometheus")

    def test_stop_request_forwards_interrupt_to_active_cohort(self) -> None:
        process = mock.Mock()
        process.poll.return_value = None
        soak._ACTIVE_RUN = process
        soak._STOP_REQUESTED = False
        self.addCleanup(setattr, soak, "_ACTIVE_RUN", None)
        self.addCleanup(setattr, soak, "_STOP_REQUESTED", False)

        with mock.patch.object(soak, "emit"):
            soak.request_stop(soak.signal.SIGTERM, None)

        self.assertTrue(soak._STOP_REQUESTED)
        process.send_signal.assert_called_once_with(soak.signal.SIGINT)


if __name__ == "__main__":
    unittest.main()
