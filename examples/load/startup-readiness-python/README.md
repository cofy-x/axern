# Startup Readiness Python Load

This example measures Axern startup and service readiness behavior with the public Python SDK. It consumes an image scenario file produced by an external build pipeline, such as forge + Kova, and does not contain region-specific registry or cloud values.

The benchmark records:

- `sandbox_ready`: concurrent `Sandbox(image=...)` startup latency.
- `service_create_ack`: service create acknowledgement latency from the shared concurrency barrier.
- `service_replica_ready`: per-replica ready latency for both service load topologies.
- `service_ready`: per-service latency until all desired replicas are ready.
- `service_first_http`: first gateway HTTP request for every created service; any non-200 response fails the benchmark.
- `service_cleanup`: service purge or environment deletion failure; any such sample fails the benchmark.
- `metrics_summary`: optional Prometheus P50/P95/P99 histogram deltas and counter deltas computed from before/after stage snapshots. Besides control, node, readiness, and gateway stages, it records imagefsd filesystem/cache latency and bytes plus Dragonfly Seed Client proxy, backend, task, and traffic outcomes for image-path attribution.
- `metrics_summary.controld_service_allocation_queue`: durable `claim_store`, `due_lag`, eligible `claim_wait`, `dispatcher_wait`, and eligibility-to-dispatch `total`. `claim_wait`, dispatcher wait, and total are required for every service-stage allocation.
- `metrics_summary.controld_service_transaction_stage`: service transaction pool acquisition (`begin`), body, commit, and total latency. Use it with the allocation queue stages to distinguish database-pool contention from scheduler or dispatcher backpressure.
- `metrics_summary.controld_service_reconcile_stage`: keyed event queue wait, service worker queue wait, sync, and total latency used to distinguish service fanout pressure from allocation startup cost. The separate `axern_controld_service_reconcile_queue_overflow_total` counter identifies fallback full sweeps. Fast periodic work is autoscaling-only; pending/retry recovery runs at startup and on a separate 30-second safety sweep.
- `summary.node_counts`: per-node sample distribution for spotting scheduler concentration during concurrent stages.

Service phases are explicit load topologies:

- `service-fanout`: `N` services with one replica each, released through one concurrency barrier. This is the primary baseline for many independently managed services.
- `service-grouped-scale`: `N / G` services with `G` replicas each, where `G` defaults to `4` and is configured by `AXERN_STARTUP_GROUPED_REPLICAS_PER_SERVICE`. The stage remains the total instance count, so results compare directly with fanout and replica-scale.
- `service-replica-scale`: one service with `N` replicas. This isolates rollout aggregation, projection, and hot-service scaling behavior.

Each service stage creates one environment before the timed concurrency barrier. After all first requests finish, it purges services concurrently and deletes the environment before taking the Prometheus after-snapshot. This lets short-lived producers such as imagefsd flush their final counters on shutdown. Startup, request, and cleanup instruments use distinct stage labels, while environment resolution and teardown remain outside the client startup latency.

Service readiness uses the resumable `WatchService` stream. Every newer service projection wakes the benchmark, which then reads current replica details and records newly ready replicas. This preserves per-replica measurements without adding client-side polling quantization to the startup latency.

The Python SDK `Sandbox` helper is backed by a transient Service, so both
benchmark phases require `owner_type=service` admission and replica-ready
samples. Sandbox scenarios do not define a readiness probe; the explicit
service phases additionally require HTTP readiness, external-port, gateway
proxy, and node proxy samples.

## Configure

Copy the template to an ignored local file and point it at a generated scenario file:

```bash
cp examples/load/startup-readiness-python/axern.env.example work/axern-startup-readiness.env
$EDITOR work/axern-startup-readiness.env
```

Run from the repository root:

```bash
set -a
source work/axern-startup-readiness.env
set +a
uv run --package axern-sdk python examples/load/startup-readiness-python/readiness.py
```

The script clears local proxy environment variables by default before opening control-plane gRPC and gateway HTTP connections.

Set `AXERN_STARTUP_PROMETHEUS_URL` when the deployment Prometheus API is reachable. This should be the metrics store used by production dashboards, not axnoded's local `/debug/metricsz` diagnostic snapshot. Without it, the benchmark only emits client-side measurements. The benchmark polls until controld replica-ready, allocation claim-wait, allocation dispatcher-wait, allocation queue total, axnoded runtime-launch, readiness-wait, applicable readiness stages, and both first-request proxy hops contain the expected allocation count (`N` for both service topologies). Histogram and counter deltas are reset-aware per producing instance before cluster aggregation, so deployment rollouts do not create negative deltas or hide samples. Ephemeral imagefsd mount identities and Seed Client pod identities are used only for this reset handling and are not emitted in aggregated summaries. `AXERN_STARTUP_METRICS_TIMEOUT_SECONDS` defaults to 45 seconds, above the Helm chart's 15-second `OTEL_METRIC_EXPORT_INTERVAL`; every `metrics_summary` reports `complete` and the observed requirements. A failed baseline query, timeout, or incomplete summary makes the benchmark exit non-zero.

Set `AXERN_STARTUP_REQUIRED_COUNTER_METRICS` to a comma-separated list of known counter summary names when a scenario's validity depends on observing a positive delta. This extends the same completeness poll; it does not sleep for an arbitrary settle period. The regional Nydus full-scan matrix requires `imagefsd_fs_read_bytes` for the cold run. A warm run may legitimately produce no imagefsd read delta when warm-rootfs or kernel page-cache reuse serves the workload without another FUSE/backend read.

Set `AXERN_STARTUP_NODE_SELECTOR_JSON` to a JSON string map only for controlled placement diagnostics, for example `{"kubernetes.io/hostname":"node-a"}`. The default is `{}` and production baselines must leave placement unconstrained so scheduler balance remains part of the gate.

The output is JSON lines. Summaries are grouped by `phase/topology/scenario/stage`. A non-zero exit means a startup, metrics-completeness, or cleanup sample failed. Created services and environments are cleaned up unless the process is interrupted hard.

## Lifecycle Soak

`lifecycle_soak.py` repeatedly runs the strict `service-fanout` lifecycle until a real wall-clock duration has elapsed. Each cohort performs create, ready, first HTTP, metrics completeness, service purge, and environment cleanup. It stops on the first failed cohort and records Prometheus resource snapshots before and after the run.

```bash
AXERN_STARTUP_SCENARIOS=tiny-go-http \
AXERN_SOAK_DURATION_SECONDS=3600 \
AXERN_SOAK_COHORT_SIZE=36 \
AXERN_SOAK_KUBECONFIG=/absolute/path/to/target.kubeconfig \
AXERN_SOAK_EXPECTED_CONTEXT=target-context-name \
AXERN_SOAK_OUTPUT_DIR=work/axern-lifecycle-soak \
uv run --package axern-sdk python \
  examples/load/startup-readiness-python/lifecycle_soak.py
```

The Prometheus URL and an explicit `AXERN_SOAK_KUBECONFIG` are mandatory. Prometheus records durable queue and node-storage state; the Kubernetes Metrics API records per-container CPU and memory. The final snapshot waits for the allocation reconcile queue to drain. Snapshot failures are benchmark failures because a soak result without resource-lifecycle evidence cannot establish stability.

For a Kubernetes Job running inside the target cluster, set
`AXERN_SOAK_IN_CLUSTER=true` and omit both `AXERN_SOAK_KUBECONFIG` and
`AXERN_SOAK_EXPECTED_CONTEXT`. The runner reads the Metrics API with its mounted
ServiceAccount token. External runs must continue to provide an explicit
kubeconfig and expected context; the two access modes cannot be combined.

## SLO Gate

`evaluate_slo.py` applies a machine-readable policy to one or more benchmark JSONL files. It checks success and failure counts, P95 latency, node distribution skew, and server metrics completeness.

```bash
uv run --package axern-sdk python \
  examples/load/startup-readiness-python/evaluate_slo.py \
  --policy examples/load/startup-readiness-python/warm-capacity-slo.json \
  --input work/fanout-36.jsonl \
  --input work/fanout-72.jsonl \
  --input work/fanout-144.jsonl \
  --input work/fanout-288.jsonl
```

The checked-in policy is the warm `tiny-go-http` capacity regression contract. Cold OCI and Nydus distribution tests use separate baselines because they measure different image paths.

Use `warm-soak-slo.json` with every cohort JSONL from a lifecycle soak. Repeated observations with the same stage are each evaluated; one slow or incomplete cohort fails the gate. Capacity runs enforce per-cohort node skew after an idle queue, while continuous churn enforces aggregate node-share deviation because asynchronous deletion intentionally overlaps the next create wave.

`capacity_probe.py` is the explicit saturation contract. `all_ready` stages require every service to become ready; `saturation` stages require a mix of ready services and services carrying the structured `WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED` diagnostic. Timeouts and message-string matching are never accepted as capacity rejection.

`steady_state.py` is an open-loop lifecycle model. It rotates all selected scenarios at a fixed arrival rate, holds each ready service for a configured lifetime, exercises the first gateway request, and then deletes and purges it. Scheduling lag and harness backpressure are first-class failures, so client-side serialization cannot silently turn an intended open-loop test into a closed-loop test.

The harness streams every lifecycle to JSONL while retaining only exact percentile samples and low-cardinality counters in memory. `steady_soak.py` adds an isolated canary, before/after resource snapshots, idle boundaries, and signal-aware cleanup for long-running in-cluster Jobs. `evaluate_steady_slo.py` reads that JSONL incrementally and turns the final summary into a release gate; `mixed-high-water-slo.json` is the validated 12 services/s, six-runtime-node policy.

Set `AXERN_STEADY_RUN_ID` for externally managed runs. The value is attached to every service as `axern.load.run`, so an interrupted Job has an exact ownership boundary for diagnosis and cleanup. A graceful cancellation drains all active lifecycle tasks before deleting their environments.

`mixed-warm-soak-slo.json` is the corresponding gate for a cohort containing
both `tiny-go-http-oci` and `tiny-go-http-nydus`. A fixed-image lifecycle soak
becomes node- and distribution-warm after its first cohort; distribution-cold
coverage remains the responsibility of the unique-image regional matrix.
The lifecycle runner therefore executes one unmeasured warmup cohort by
default. Warmup JSONL remains in the result directory for diagnosis, while the
configured observation duration and SLO evaluation cover only `cohort-*.jsonl`.
Each OCI/Nydus scenario runs as an isolated wave, and the runner waits for the
allocation reconcile queue and active allocation count to both reach zero
between waves. This prevents asynchronous node release from one image format
from changing the placement baseline of the next.
