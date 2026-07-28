import concurrent.futures
import importlib.util
import io
import json
from pathlib import Path
import os
import sys
import time
from types import SimpleNamespace
import unittest
from unittest import mock

import grpc

from axern.control.common.v1 import common_pb2
from axern.control.service.v1 import service_replica_pb2, service_types_pb2


MODULE_PATH = Path(__file__).with_name("readiness.py")
SPEC = importlib.util.spec_from_file_location("startup_readiness", MODULE_PATH)
assert SPEC is not None and SPEC.loader is not None
readiness = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = readiness
SPEC.loader.exec_module(readiness)


class MetricsRequirementsTest(unittest.TestCase):
    def test_node_selector_json_requires_string_map(self) -> None:
        self.assertEqual(
            readiness.parse_string_map_json(
                '{" kubernetes.io/hostname ": " node-a "}',
                "AXERN_STARTUP_NODE_SELECTOR_JSON",
            ),
            {"kubernetes.io/hostname": "node-a"},
        )
        for value in (
            '[]',
            '{"node": 1}',
            '{"node": ""}',
            '{"": "node-a"}',
            '{"node": "a", " node ": "b"}',
            '{invalid',
        ):
            with self.subTest(value=value), self.assertRaises(SystemExit):
                readiness.parse_string_map_json(value, "AXERN_STARTUP_NODE_SELECTOR_JSON")

    def test_sandbox_requires_control_and_node_startup_metrics(self) -> None:
        metrics = startup_metrics(probe_type="unknown", samples=2)
        metrics["controld_service_replica_ready"] = [
            {"labels": {}, "samples": 2.0},
        ]

        requirements = readiness.metrics_requirements(
            "sandbox_stage",
            2,
            metrics,
        )

        self.assertEqual(
            set(requirements),
            {
                "controld_resource_admission",
                "controld_replica_ready",
                "axnoded_runtime_launch",
            },
        )
        self.assertTrue(readiness.requirements_complete(requirements))

    def test_concurrent_events_remain_valid_jsonl(self) -> None:
        output = io.StringIO()
        with mock.patch("sys.stdout", output), concurrent.futures.ThreadPoolExecutor(
            max_workers=16
        ) as executor:
            futures = [
                executor.submit(readiness.emit, "sample", {"index": index}) for index in range(200)
            ]
            for future in futures:
                future.result()

        events = [json.loads(line) for line in output.getvalue().splitlines()]
        self.assertEqual(len(events), 200)
        self.assertEqual({event["index"] for event in events}, set(range(200)))

    def test_service_also_requires_controld_replica_ready(self) -> None:
        metrics = startup_metrics(probe_type="http", samples=2)
        metrics["controld_service_replica_ready"] = [
            {"labels": {}, "samples": 2.0},
        ]

        requirements = readiness.metrics_requirements(
            "service_stage",
            2,
            metrics,
            service_request_count=2,
        )

        self.assertEqual(
            set(requirements),
            {
                "controld_resource_admission",
                "controld_replica_ready",
                "controld_allocation_claim_wait",
                "controld_allocation_dispatcher_wait",
                "controld_allocation_queue_total",
                "axnoded_runtime_launch",
                "axnoded_readiness_wait",
                "axnoded_readiness_sandbox_stage",
                "axnoded_readiness_external_port_stage",
                "gateway_service_proxy_total",
                "axnoded_http_proxy_total",
                "axnoded_execution_lease_visibility",
            },
        )
        self.assertTrue(readiness.requirements_complete(requirements))

    def test_incomplete_node_samples_fail_the_contract(self) -> None:
        metrics = startup_metrics(probe_type="unknown", samples=1)
        metrics["controld_service_replica_ready"] = [
            {"labels": {}, "samples": 2.0},
        ]

        requirements = readiness.metrics_requirements(
            "sandbox_stage",
            2,
            metrics,
        )

        self.assertFalse(readiness.requirements_complete(requirements))

    def test_service_requires_allocation_queue_stage_samples(self) -> None:
        metrics = startup_metrics(probe_type="http", samples=2)
        metrics["controld_service_replica_ready"] = [{"labels": {}, "samples": 2.0}]
        del metrics["controld_service_allocation_queue"][1]

        requirements = readiness.metrics_requirements(
            "service_stage",
            2,
            metrics,
            service_request_count=2,
        )

        self.assertFalse(requirements["controld_allocation_dispatcher_wait"]["complete"])
        self.assertFalse(readiness.requirements_complete(requirements))

    def test_replica_scale_requires_one_first_request_but_all_startup_samples(self) -> None:
        metrics = startup_metrics(probe_type="http", samples=12)
        metrics["controld_service_replica_ready"] = [{"labels": {}, "samples": 12.0}]
        metrics["gateway_service_proxy_stage"] = [
            {"labels": {"axern_stage": "total"}, "samples": 1.0}
        ]
        metrics["axnoded_http_proxy_stage"] = [
            {"labels": {"axern_stage": "total"}, "samples": 1.0}
        ]
        metrics["axnoded_execution_lease_visibility"] = [
            {"labels": {"axern_result": "event_wait"}, "samples": 1.0}
        ]

        requirements = readiness.metrics_requirements(
            "service_stage",
            12,
            metrics,
            service_request_count=1,
        )

        self.assertTrue(readiness.requirements_complete(requirements))
        self.assertEqual(requirements["gateway_service_proxy_total"]["expected"], 1.0)

    def test_sandbox_requires_service_owned_admission_samples(self) -> None:
        metrics = startup_metrics(probe_type="unknown", samples=2)
        metrics["controld_resource_admission_stage"] = [
            {
                "labels": {
                    "axern_owner_type": "run",
                    "axern_stage": "total",
                },
                "samples": 2.0,
            },
        ]
        metrics["controld_service_replica_ready"] = [
            {"labels": {}, "samples": 2.0},
        ]

        requirements = readiness.metrics_requirements("sandbox_stage", 2, metrics)

        self.assertFalse(requirements["controld_resource_admission"]["complete"])

    def test_unknown_scope_is_rejected(self) -> None:
        with self.assertRaisesRegex(ValueError, "unsupported metrics scope"):
            readiness.metrics_requirements("unknown", 1, {})

    def test_explicit_counter_delta_participates_in_completeness(self) -> None:
        metrics = startup_metrics(probe_type="unknown", samples=1)
        metrics["controld_service_replica_ready"] = [{"labels": {}, "samples": 1.0}]
        metrics["imagefsd_fs_read_bytes"] = [{"labels": {"result": "ok"}, "value": 4096.0}]

        requirements = readiness.metrics_requirements(
            "sandbox_stage",
            1,
            metrics,
            required_counter_metrics=("imagefsd_fs_read_bytes",),
        )

        self.assertTrue(requirements["counter_delta.imagefsd_fs_read_bytes"]["complete"])
        self.assertTrue(readiness.requirements_complete(requirements))

    def test_histogram_delta_handles_counter_reset(self) -> None:
        self.assertEqual(
            readiness.delta_buckets(
                {"0.5": 20.0, "+Inf": 25.0},
                {"0.5": 2.0, "+Inf": 3.0},
            ),
            {"0.5": 2.0, "+Inf": 3.0},
        )

    def test_histogram_delta_preserves_normal_increment(self) -> None:
        self.assertEqual(
            readiness.delta_buckets(
                {"0.5": 20.0, "+Inf": 25.0},
                {"0.5": 22.0, "+Inf": 28.0},
            ),
            {"0.5": 2.0, "+Inf": 3.0},
        )

    def test_histogram_delta_handles_one_node_reset_before_cluster_aggregation(self) -> None:
        spec = readiness.MetricSpec(
            name="startup",
            metric="startup_bucket",
            groups=("phase",),
            series_groups=("node",),
        )
        before = [
            histogram("launch", "node-a", 20.0, 25.0),
            histogram("launch", "node-b", 10.0, 12.0),
        ]
        after = [
            histogram("launch", "node-a", 2.0, 3.0),
            histogram("launch", "node-b", 12.0, 14.0),
        ]

        summaries = readiness.summarize_histogram_delta(before, after, spec)

        self.assertEqual(len(summaries), 1)
        self.assertEqual(summaries[0]["labels"], {"phase": "launch"})
        self.assertEqual(summaries[0]["samples"], 5.0)

    def test_counter_delta_handles_restart_before_cluster_aggregation(self) -> None:
        spec = readiness.CounterSpec(
            name="traffic",
            metric="traffic_total",
            groups=("route",),
            series_groups=("pod",),
        )
        before = [
            counter("origin", "seed-a", 20.0),
            counter("origin", "seed-b", 10.0),
        ]
        after = [
            counter("origin", "seed-a", 2.0),
            counter("origin", "seed-b", 14.0),
        ]

        summaries = readiness.summarize_counter_delta(before, after, spec)

        self.assertEqual(summaries, [{"labels": {"route": "origin"}, "value": 6.0}])

    def test_counter_delta_omits_unchanged_series(self) -> None:
        spec = readiness.CounterSpec("traffic", "traffic_total", (), ("pod",))

        summaries = readiness.summarize_counter_delta(
            [{"labels": {"pod": "seed-a"}, "value": 20.0}],
            [{"labels": {"pod": "seed-a"}, "value": 20.0}],
            spec,
        )

        self.assertEqual(summaries, [])

    def test_gauge_snapshot_aggregates_instances_without_delta(self) -> None:
        spec = readiness.GaugeSpec(
            "decoded_bytes",
            "decoded_bytes",
            (),
            ("instance",),
        )

        summaries = readiness.summarize_gauge_samples(
            [
                {"labels": {"instance": "imagefsd-a"}, "value": 1024.0},
                {"labels": {"instance": "imagefsd-b"}, "value": 2048.0},
            ],
            spec,
        )

        self.assertEqual(summaries, [{"labels": {}, "value": 3072.0}])

    def test_prometheus_selector_quotes_matcher_values(self) -> None:
        self.assertEqual(
            readiness.prometheus_selector("requests_total", (("job", 'seed"client'),)),
            'requests_total{job="seed\\"client"}',
        )

    def test_dragonfly_seed_summaries_retain_pod_attribution(self) -> None:
        specs = (*readiness.METRIC_SPECS, *readiness.COUNTER_SPECS)
        dragonfly_specs = [spec for spec in specs if spec.name.startswith("dragonfly_seed_")]

        self.assertTrue(dragonfly_specs)
        for spec in dragonfly_specs:
            with self.subTest(metric=spec.name):
                self.assertIn("pod", spec.groups)
                self.assertNotIn("pod", spec.series_groups)

        task_counter = next(spec for spec in readiness.COUNTER_SPECS if spec.name == "dragonfly_seed_download_tasks")
        self.assertEqual(task_counter.groups, ("pod", "type", "priority"))

    def test_imagefsd_cold_path_summaries_retain_node_attribution(self) -> None:
        names = {"imagefsd_cache_backend_fetch", "imagefsd_cache_inflight_wait"}
        specs = [spec for spec in readiness.METRIC_SPECS if spec.name in names]

        self.assertEqual({spec.name for spec in specs}, names)
        for spec in specs:
            with self.subTest(metric=spec.name):
                self.assertIn("node_id", spec.groups)
                self.assertEqual(spec.series_groups, ("exported_instance", "node_id"))


class ConfigValidationTest(unittest.TestCase):
    def test_float_config_rejects_non_finite_values(self) -> None:
        for value in ("nan", "inf", "-inf"):
            with self.subTest(value=value):
                with self.assertRaises(SystemExit):
                    readiness.positive_float(value, "TEST_VALUE")
                with self.assertRaises(SystemExit):
                    readiness.nonnegative_float(value, "TEST_VALUE")

    def test_disable_proxy_env_clears_all_proxy_variables(self) -> None:
        names = (
            "HTTP_PROXY",
            "HTTPS_PROXY",
            "ALL_PROXY",
            "NO_PROXY",
            "http_proxy",
            "https_proxy",
            "all_proxy",
            "no_proxy",
        )
        with mock.patch.dict(os.environ, {name: "configured" for name in names}, clear=False):
            readiness.disable_proxy_env()
            self.assertTrue(all(name not in os.environ for name in names))


class ServiceTopologyTest(unittest.TestCase):
    def test_fanout_creates_one_replica_services(self) -> None:
        self.assertEqual(readiness.service_topology_shape("service-fanout", 36), (36, 1))

    def test_replica_scale_creates_one_multi_replica_service(self) -> None:
        self.assertEqual(readiness.service_topology_shape("service-replica-scale", 36), (1, 36))

    def test_grouped_scale_preserves_total_instances(self) -> None:
        self.assertEqual(readiness.service_topology_shape("service-grouped-scale", 36, 4), (9, 4))

    def test_grouped_scale_rejects_partial_group(self) -> None:
        with self.assertRaisesRegex(ValueError, "must be divisible"):
            readiness.service_topology_shape("service-grouped-scale", 35, 4)

    def test_unknown_topology_is_rejected(self) -> None:
        with self.assertRaisesRegex(ValueError, "unsupported service topology"):
            readiness.service_topology_shape("service", 36)


class GatewayHTTPMeasurementTest(unittest.TestCase):
    def setUp(self) -> None:
        self.scenario = readiness.Scenario(
            name="tiny",
            image_ref="example.com/tiny:latest",
            port=8080,
            path="/ready",
            expected_body="ready",
            sandbox_argv=(),
            service_argv=(),
        )
        self.config = SimpleNamespace(
            service_url="http://127.0.0.1:25080",
            namespace="default",
        )

    def test_first_http_records_client_transport_stages(self) -> None:
        response = mock.Mock()
        response.status = 200
        response.read.return_value = b"ready"
        connection = mock.Mock()
        connection.getresponse.return_value = response
        with mock.patch.object(readiness.http.client, "HTTPConnection", return_value=connection), mock.patch.object(
            readiness.time,
            "monotonic",
            side_effect=(10.0, 10.2, 10.3, 10.7, 11.2, 11.2),
        ):
            result = readiness.fetch_first_gateway_http(
                self.config,
                self.scenario,
                "service-fanout",
                "svc-a",
                1,
                1,
                0,
                0.0,
            )

        self.assertTrue(result.ok)
        connection.connect.assert_called_once_with()
        connection.request.assert_called_once_with("GET", "/svc/default/svc-a/8080/ready")
        self.assertEqual(result.client_total_seconds, 1.2)
        self.assertEqual(result.client_connect_seconds, 0.2)
        self.assertEqual(result.client_request_write_seconds, 0.1)
        self.assertEqual(result.client_response_headers_seconds, 0.4)
        self.assertEqual(result.client_response_body_seconds, 0.5)

    def test_first_http_attributes_connect_failure(self) -> None:
        connection = mock.Mock()
        connection.connect.side_effect = TimeoutError("connect timed out")
        with mock.patch.object(readiness.http.client, "HTTPConnection", return_value=connection), mock.patch.object(
            readiness.time,
            "monotonic",
            side_effect=(20.0, 21.5, 21.5),
        ):
            result = readiness.fetch_first_gateway_http(
                self.config,
                self.scenario,
                "service-fanout",
                "svc-a",
                1,
                1,
                0,
                0.0,
            )

        self.assertFalse(result.ok)
        self.assertEqual(result.client_error_stage, "connect")
        self.assertEqual(result.client_total_seconds, 1.5)
        self.assertEqual(result.client_connect_seconds, 1.5)


class ServiceReadyWatchTest(unittest.TestCase):
    def test_service_creation_forwards_node_selector(self) -> None:
        client = mock.Mock()
        client.create_service.return_value = SimpleNamespace(id="svc-a")
        config = SimpleNamespace(
            namespace="default",
            runtime_class="runsc",
            request_cpu="500m",
            request_memory="512Mi",
            limit_cpu="1",
            limit_memory="1Gi",
            node_selector={"kubernetes.io/hostname": "node-a"},
        )
        scenario = readiness.Scenario(
            name="tiny",
            image_ref="example.com/tiny:latest",
            port=8080,
            path="/ready",
            expected_body="ok",
            sandbox_argv=(),
            service_argv=("/tiny-http",),
        )

        with mock.patch.object(readiness, "emit"), mock.patch.object(
            readiness,
            "wait_ready_replicas",
            return_value=([], ""),
        ), mock.patch.object(
            readiness,
            "fetch_first_gateway_http",
            return_value=readiness.HTTPMeasurement(
                ok=True,
                phase="service_first_http",
                topology="service-fanout",
                scenario="tiny",
                stage=1,
                iteration=1,
                index=0,
                elapsed_seconds=0.1,
            ),
        ):
            attempt = readiness.run_one_service(
                config,
                client,
                scenario,
                "service-fanout",
                "env-a",
                1,
                1,
                0,
                1,
                mock.Mock(wait=mock.Mock()),
                {"started": time.monotonic()},
            )

        self.assertEqual(attempt.service_id, "svc-a")
        self.assertEqual(
            client.create_service.call_args.kwargs["node_selector"],
            {"kubernetes.io/hostname": "node-a"},
        )

    def test_replica_details_are_read_after_service_watch_updates(self) -> None:
        client = WatchReadyClient()
        scenario = readiness.Scenario(
            name="tiny",
            image_ref="example.com/tiny:latest",
            port=8080,
            path="/ready",
            expected_body="ok",
            sandbox_argv=(),
            service_argv=(),
        )

        with mock.patch.object(readiness, "emit"):
            measurements, error = readiness.wait_ready_replicas(
                SimpleNamespace(ready_timeout_seconds=5.0),
                client,
                scenario,
                "service-fanout",
                "svc-a",
                1,
                1,
                0,
                1,
                time.monotonic(),
            )

        self.assertEqual(error, "")
        self.assertEqual(len(measurements), 1)
        self.assertEqual(measurements[0].allocation_id, "alloc-a")
        self.assertEqual(client.list_calls, 2)

    def test_transient_list_timeout_reconnects_watch_from_last_version(self) -> None:
        client = ReconnectingWatchReadyClient()
        scenario = readiness.Scenario(
            name="tiny",
            image_ref="example.com/tiny:latest",
            port=8080,
            path="/ready",
            expected_body="ok",
            sandbox_argv=(),
            service_argv=(),
        )

        with mock.patch.object(readiness, "emit"), mock.patch.object(readiness.time, "sleep"):
            measurements, error = readiness.wait_ready_replicas(
                SimpleNamespace(ready_timeout_seconds=5.0),
                client,
                scenario,
                "service-fanout",
                "svc-a",
                1,
                1,
                0,
                1,
                time.monotonic(),
            )

        self.assertEqual(error, "")
        self.assertEqual(len(measurements), 1)
        self.assertEqual(client.after_versions, [0, 0])
        self.assertEqual(client.list_calls, 2)


class CleanupTest(unittest.TestCase):
    def test_service_stage_cleans_up_before_final_metrics_snapshot(self) -> None:
        events: list[str] = []
        client = mock.Mock()
        client.create_environment.return_value = SimpleNamespace(id="env-a")
        client.close.side_effect = lambda: events.append("client_close")
        scenario = readiness.Scenario(
            name="tiny",
            image_ref="example.com/tiny:latest",
            port=8080,
            path="/ready",
            expected_body="ok",
            sandbox_argv=(),
            service_argv=("/tiny-http",),
        )

        def run_attempts(*_args: object) -> list[object]:
            events.append("workload")
            return []

        def cleanup(*_args: object) -> list[tuple[str, str, str]]:
            events.append("cleanup")
            return []

        def snapshot(*_args: object) -> bool:
            events.append("metrics")
            return True

        with mock.patch.object(readiness, "control_client", return_value=client), mock.patch.object(
            readiness,
            "run_service_attempts",
            side_effect=run_attempts,
        ), mock.patch.object(
            readiness,
            "cleanup_service_stage",
            side_effect=cleanup,
        ), mock.patch.object(
            readiness,
            "emit_metrics_summary",
            side_effect=snapshot,
        ), mock.patch.object(readiness, "emit"):
            results, metrics_complete = readiness.run_service_stage(
                SimpleNamespace(namespace="default", registry_credential_id=""),
                scenario,
                "service-fanout",
                1,
                1,
                mock.sentinel.metrics_before,
            )

        self.assertEqual(results, [])
        self.assertTrue(metrics_complete)
        self.assertEqual(events, ["workload", "cleanup", "client_close", "metrics"])

    def test_cleanup_failures_are_returned_to_the_benchmark(self) -> None:
        client = FailingCleanupClient()

        with mock.patch.object(readiness, "emit"):
            failures = readiness.cleanup_service_stage(client, ["svc-a"], "env-a")

        self.assertEqual(
            failures,
            [
                ("service", "svc-a", "RuntimeError: delete service failed"),
                ("environment", "env-a", "RuntimeError: delete environment failed"),
            ],
        )

    def test_sandbox_cleanup_reports_success_after_verified_purge(self) -> None:
        client = SandboxCleanupClient()
        sandbox = FakeSandbox("env-a")
        attempt = sandbox_attempt(sandbox, client)

        with mock.patch.object(readiness, "emit"):
            result = readiness.cleanup_one_sandbox(attempt, "tiny", 1, 1)

        self.assertTrue(result.ok)
        self.assertEqual(result.phase, "sandbox_cleanup")
        self.assertEqual(client.deleted_services, ["svc-a"])
        self.assertEqual(client.purged_services, ["svc-a"])
        self.assertEqual(client.deleted_environments, ["env-a"])
        self.assertTrue(sandbox.closed)
        self.assertTrue(client.closed)

    def test_sandbox_cleanup_surfaces_resource_failures(self) -> None:
        client = FailingSandboxCleanupClient()
        sandbox = FakeSandbox("env-a")
        attempt = sandbox_attempt(sandbox, client)

        with mock.patch.object(readiness, "emit"):
            result = readiness.cleanup_one_sandbox(attempt, "tiny", 1, 1)

        self.assertFalse(result.ok)
        self.assertIn("service svc-a", result.error)
        self.assertIn("environment env-a", result.error)
        self.assertTrue(sandbox.closed)
        self.assertTrue(client.closed)

    def test_cleanup_does_not_retry_unexpected_purge_errors(self) -> None:
        client = UnexpectedPurgeFailureClient()

        with mock.patch.object(readiness, "emit"), mock.patch.object(readiness.time, "sleep") as sleep:
            service_id, error = readiness.cleanup_service(client, "svc-a")

        self.assertEqual(service_id, "svc-a")
        self.assertIn("PERMISSION_DENIED", error)
        sleep.assert_not_called()


def startup_metrics(probe_type: str, samples: float) -> dict[str, list[dict[str, object]]]:
    return {
        "controld_service_allocation_queue": [
            {
                "labels": {
                    "axern_path": "reconcile_create",
                    "axern_stage": stage,
                    "axern_result": "ok",
                },
                "samples": samples,
            }
            for stage in ("claim_wait", "dispatcher_wait", "total")
        ],
        "controld_resource_admission_stage": [
            {
                "labels": {
                    "axern_owner_type": "service",
                    "axern_stage": "total",
                },
                "samples": samples,
            },
        ],
        "axnoded_startup_phase": [
            {
                "labels": {"axern_phase": "runtime_launch"},
                "samples": samples,
            },
        ],
        "axnoded_readiness_wait": [
            {
                "labels": {"axern_probe_type": probe_type},
                "samples": samples,
            },
        ],
        "axnoded_readiness_probe_stage": [
            {
                "labels": {
                    "axern_probe_type": probe_type,
                    "axern_stage": "sandbox",
                },
                "samples": samples,
            },
            {
                "labels": {
                    "axern_probe_type": "http",
                    "axern_stage": "external_port",
                },
                "samples": samples,
            },
        ],
        "gateway_service_proxy_stage": [
            {
                "labels": {"axern_stage": "total"},
                "samples": samples,
            },
        ],
        "axnoded_http_proxy_stage": [
            {
                "labels": {"axern_stage": "total"},
                "samples": samples,
            },
        ],
        "axnoded_execution_lease_visibility": [
            {
                "labels": {"axern_result": "event_wait"},
                "samples": samples,
            },
        ],
    }


def histogram(phase: str, node: str, finite: float, total: float) -> dict[str, object]:
    return {
        "labels": {"phase": phase, "node": node},
        "buckets": {"0.5": finite, "+Inf": total},
    }


def counter(route: str, pod: str, value: float) -> dict[str, object]:
    return {
        "labels": {"route": route, "pod": pod},
        "value": value,
    }


class WatchReadyClient:
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
            ready_replicas=1,
        )

    def list_service_replicas(self, service_id: str, **kwargs):
        del kwargs
        self.list_calls += 1
        return [
            service_replica_pb2.ServiceReplica(
                id="alloc-a",
                service_id=service_id,
                node_id="node-a",
                status=common_pb2.ALLOCATION_STATUS_RUNNING,
                ready=self.list_calls == 2,
            )
        ]


class ReconnectingWatchReadyClient:
    def __init__(self) -> None:
        self.after_versions: list[int] = []
        self.list_calls = 0

    def watch_service(self, service_id: str, *, after_version: int, **kwargs):
        del kwargs
        self.after_versions.append(after_version)
        yield service_types_pb2.Service(
            id=service_id,
            version=1,
            status=service_types_pb2.SERVICE_STATUS_READY,
        )

    def list_service_replicas(self, service_id: str, **kwargs):
        del kwargs
        self.list_calls += 1
        if self.list_calls == 1:
            raise TimeoutError("control endpoint recovering")
        return [
            service_replica_pb2.ServiceReplica(
                id="alloc-a",
                service_id=service_id,
                node_id="node-a",
                status=common_pb2.ALLOCATION_STATUS_RUNNING,
                ready=True,
            )
        ]


class FailingCleanupClient:
    def delete_service(self, service_id: str, timeout: float) -> None:
        raise RuntimeError("delete service failed")

    def delete_environment(self, environment_id: str, timeout: float) -> None:
        raise RuntimeError("delete environment failed")


class FakeSandbox:
    def __init__(self, environment_id: str) -> None:
        self.metadata = type("Metadata", (), {"environment_id": environment_id})()
        self.closed = False

    def close(self) -> None:
        self.closed = True


class SandboxCleanupClient:
    def __init__(self) -> None:
        self.deleted_services: list[str] = []
        self.purged_services: list[str] = []
        self.deleted_environments: list[str] = []
        self.closed = False

    def delete_service(self, service_id: str, timeout: float) -> None:
        self.deleted_services.append(service_id)

    def admin_purge_service(self, service_id: str, *, operator_reason: str, timeout: float) -> None:
        del operator_reason, timeout
        self.purged_services.append(service_id)

    def delete_environment(self, environment_id: str, timeout: float) -> None:
        self.deleted_environments.append(environment_id)

    def close(self) -> None:
        self.closed = True


class FailingSandboxCleanupClient(SandboxCleanupClient):
    def delete_service(self, service_id: str, timeout: float) -> None:
        raise RuntimeError("delete service failed")

    def delete_environment(self, environment_id: str, timeout: float) -> None:
        raise RuntimeError("delete environment failed")


class PurgeRPCError(grpc.RpcError):
    def code(self):
        return grpc.StatusCode.PERMISSION_DENIED

    def __str__(self) -> str:
        return "PERMISSION_DENIED"


class UnexpectedPurgeFailureClient:
    def delete_service(self, service_id: str, timeout: float) -> None:
        del service_id, timeout

    def admin_purge_service(self, service_id: str, *, operator_reason: str, timeout: float) -> None:
        del service_id, operator_reason, timeout
        raise PurgeRPCError()


def sandbox_attempt(sandbox: FakeSandbox, client: SandboxCleanupClient):
    return readiness.SandboxAttempt(
        measurement=readiness.Measurement(
            ok=True,
            phase="sandbox_ready",
            topology="sandbox",
            scenario="tiny",
            stage=1,
            iteration=1,
            index=0,
            elapsed_seconds=1.0,
            allocation_id="alloc-a",
            node_id="node-a",
            service_id="svc-a",
        ),
        sandbox=sandbox,
        client=client,
    )


if __name__ == "__main__":
    unittest.main()
