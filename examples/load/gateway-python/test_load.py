import importlib.util
from pathlib import Path
import sys
from types import SimpleNamespace
import unittest
from unittest import mock

import grpc

from axern.control.common.v1 import common_pb2
from axern.control.service.v1 import service_types_pb2


MODULE_PATH = Path(__file__).with_name("load.py")
SPEC = importlib.util.spec_from_file_location("gateway_load", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
load = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = load
SPEC.loader.exec_module(load)


class MetricsTest(unittest.TestCase):
    def test_histogram_delta_is_reset_aware_per_process(self) -> None:
        metric = "axern_gateway_service_proxy_stage_duration_seconds_bucket"
        before = {
            metric: {
                series_key("old"): {0.5: 10.0, float("inf"): 10.0},
                series_key("stable"): {0.5: 4.0, float("inf"): 4.0},
            },
        }
        after = {
            metric: {
                series_key("new"): {0.5: 2.0, float("inf"): 2.0},
                series_key("stable"): {0.5: 7.0, float("inf"): 7.0},
            },
        }

        summaries = load.summarize_metrics_delta(before, after)

        self.assertEqual(len(summaries), 1)
        self.assertEqual(summaries[0]["count"], 5)
        self.assertEqual(summaries[0]["stage"], "total")

    def test_metrics_require_both_proxy_hops(self) -> None:
        summaries = [
            {"metric": metric, "stage": "total", "count": 4}
            for metric in load.REQUIRED_HTTP_STAGE_METRICS
        ]

        requirements = load.metrics_requirements(summaries, 4)

        self.assertTrue(all(item["complete"] for item in requirements.values()))

    def test_lease_visibility_is_diagnostic_not_a_per_request_gate(self) -> None:
        summaries = [
            {"metric": metric, "stage": "total", "count": 4}
            for metric in load.REQUIRED_HTTP_STAGE_METRICS
        ]
        summaries.append({
            "metric": "axern_axnoded_execution_lease_visibility_duration_seconds_bucket",
            "stage": "",
            "result": "cache_hit",
            "count": 4,
        })

        requirements = load.metrics_requirements(summaries, 4)

        self.assertEqual(set(requirements), set(load.REQUIRED_HTTP_STAGE_METRICS))


class ReadinessTest(unittest.TestCase):
    def test_wait_ready_replicas_uses_service_watch(self) -> None:
        client = ReadyClient()

        replicas = load.wait_ready_replicas(client, "svc-a", 1, 5.0)

        self.assertEqual(len(replicas), 1)
        self.assertEqual(replicas[0].id, "alloc-a")
        self.assertEqual(client.list_calls, 2)


class CleanupTest(unittest.TestCase):
    def test_unexpected_purge_error_fails_without_retry(self) -> None:
        client = FailingPurgeClient()

        with mock.patch.object(load, "emit"), mock.patch.object(load.time, "sleep") as sleep:
            failures = load.cleanup_service(client, "svc-a", "")

        self.assertEqual(failures, 1)
        sleep.assert_not_called()


class HTTPProfileTest(unittest.TestCase):
    def test_profile_requests_have_stable_payload_contracts(self) -> None:
        self.assertEqual(load.profile_request("large"), ("/bytes?size=4194304", 4 << 20))
        self.assertEqual(
            load.profile_request("stream"),
            ("/stream?chunks=64&size=16384&delay_ms=2", 1 << 20),
        )

    def test_image_source_selects_fixture_entrypoint(self) -> None:
        env = {
            "AXERN_ENDPOINT": "control:25000",
            "AXERN_TLS_CA_CERT": "ca",
            "AXERN_TLS_CERT": "cert",
            "AXERN_TLS_KEY": "key",
            "AXERN_SERVICE_URL": "http://gateway:25080",
            "AXERN_LOAD_IMAGE_REF": "registry/fixture:tag",
            "AXERN_LOAD_HTTP_PROFILES": "keepalive,short,large,stream,lb",
        }
        with mock.patch.dict("os.environ", env, clear=True):
            config = load.config_from_env()

        self.assertEqual(config.template_id, "")
        self.assertEqual(config.image_ref, "registry/fixture:tag")
        self.assertEqual(config.service_argv, ("/usr/local/bin/axern-startup-server",))
        self.assertEqual(config.http_profiles, load.HTTP_PROFILES)

    def test_basic_http_profiles_accept_nonempty_fixture_responses(self) -> None:
        load.validate_profile_body("keepalive", b"axern-startup tiny-go-http ok\n")
        load.validate_profile_body("short", b"<!DOCTYPE HTML>")
        with self.assertRaisesRegex(RuntimeError, "empty response body"):
            load.validate_profile_body("keepalive", b"")


def series_key(instance: str) -> tuple[str, str, str, str, str, str]:
    return ("total", "ok", "none", "GET", instance, "")


class ReadyClient:
    def __init__(self) -> None:
        self.list_calls = 0

    def watch_service(self, service_id: str, **kwargs):
        del kwargs
        yield service_types_pb2.Service(
            id=service_id,
            version=1,
            status=service_types_pb2.SERVICE_STATUS_RECONCILING,
        )
        yield service_types_pb2.Service(
            id=service_id,
            version=2,
            status=service_types_pb2.SERVICE_STATUS_READY,
        )

    def list_service_replicas(self, service_id: str, **kwargs):
        del service_id, kwargs
        self.list_calls += 1
        return [
            SimpleNamespace(
                id="alloc-a",
                ready=self.list_calls == 2,
                ended=False,
                outdated=False,
                status=common_pb2.ALLOCATION_STATUS_RUNNING,
                node_id="node-a",
                message="",
            )
        ]


class PurgeRPCError(grpc.RpcError):
    def code(self):
        return grpc.StatusCode.PERMISSION_DENIED


class FailingPurgeClient:
    def delete_service(self, service_id: str, timeout: float) -> None:
        del service_id, timeout

    def admin_purge_service(self, service_id: str, *, operator_reason: str, timeout: float) -> None:
        del service_id, operator_reason, timeout
        raise PurgeRPCError()


if __name__ == "__main__":
    unittest.main()
