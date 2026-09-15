# Resource Admission

This document describes the control-plane resource admission path from placement prefiltering through authoritative PostgreSQL checks, diagnostics, and metrics.

## Core Model

Workload `resources.requests` are the Allocation admission and resource-charge contract. Workload `resources.limits` are resource-specific hard ceilings enforced by `axnoded`; CPU and memory use cgroups, while ephemeral storage uses the selected runtime's overlay quota. Limits are not quota usage and do not increase control-plane capacity.

CPU, memory, ephemeral storage, and runtime instance slots have distinct admission semantics:

| Resource | Admission behavior |
| --- | --- |
| CPU | Node allocatable CPU can be overcommitted through the global `-resource-cpu-overcommit-ratio` policy. |
| Memory | Memory remains strict. Effective allocatable is the lesser of the node resource source and finite delegated cgroup-root limit, minus the explicit node system reserve. Remaining capacity is derived only from non-released Allocation charges. |
| Ephemeral storage | Node-local filestore capacity is strict after the system reserve and non-released Allocation charges. |
| Runtime slots | One non-released Allocation consumes one slot. Axnoded reports the aggregate node-owned slot capacity after applying enabled resource-pool constraints. |

Namespace quota is a namespace cap on requests. Node admission is a node cap on requests after applying the global node policy. CPU overcommit never multiplies namespace quota.

## Admission Sequence

Admission has two evaluation points:

1. Placement prefilters eligible nodes and emits a request-scoped candidate plan. It checks freshness, readiness, capability, and whether a single request can fit total observed allocatable capacity, but it deliberately does not infer remaining capacity from node-local commitment summaries.
2. PostgreSQL resource admission locks Namespace quota and candidate Nodes, reruns complete lifecycle, freshness, runtime, component, label, typed capability, CPU, memory, ephemeral-storage, and runtime-slot eligibility, re-sums non-released Allocation charges, ranks within a preference tier from those charges, and writes the Allocation binding and charge in one transaction.

The transaction check is authoritative. Placement is an optimization and a source of candidate diagnostics, not a durable boundary. Immutable capability requirements commit with the Allocation; Node observations are not copied into a second evidence or admission record. See [Observed Capability Providers](../../../docs/architecture/observed-capability-providers.md).

Runtime-slot admission uses the strongest current occupancy signal:

```text
charged_ids = non-released Allocation IDs bound to the Node
active_ids  = axnoded.active_allocation_ids
pool_used   = runtime_slots.using
occupied    = max(len(union(charged_ids, active_ids)), pool_used)
capacity  = runtime_slots.capacity - runtime_slots.unavailable
```

This counts each Allocation once while runtime startup and Node reports move asynchronously. Resource-release failures retain the Allocation charge until cleanup succeeds. Slots that cannot be owned safely after a local creation/rollback failure are folded into `runtime_slots.unavailable`; admission never infers the aggregate contract from cgroup or interface implementation details.

`runtime_slots.idle` is the number of aggregate slots whose enabled node-local resources are already materialized and can be reused immediately. Placement may use it as a warm-start preference, but admission depends only on aggregate capacity, unavailable slots, and occupancy. A missing `runtime_slots` contract is invalid and fails admission closed.

## Structured Diagnostics

Resource admission errors use gRPC `ErrorInfo` details with domain:

```text
axern.control.resource_admission
```

Current reasons:

| Reason                                | Meaning                                                                                                                 | Typical code         |
| ------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- | -------------------- |
| `NAMESPACE_QUOTA_EXCEEDED`            | Namespace request quota would be exceeded.                                                                              | `ResourceExhausted`  |
| `NODE_CAPACITY_EXHAUSTED`           | No selected candidate has remaining CPU, memory, storage, or runtime-slot capacity after transaction recheck. The stable external reason name does not imply a Reservation entity. | `ResourceExhausted`  |
| `PLACEMENT_CAPACITY_EXHAUSTED`        | Placement rejected candidates for CPU, memory, or ephemeral-storage capacity.                                           | `FailedPrecondition` |
| `NODE_SELECTION_ERROR`                | Placement found no eligible node for non-capacity reasons such as runtime, selector, readiness, or capability mismatch. | `FailedPrecondition` |

Human-readable error messages remain stable enough for operators, but workload state, CLI rendering, SDKs, and future clients should prefer `axern.control.common.v1.WorkloadDiagnosticCode` when it is available. Run views expose that shared diagnostic code directly. Quota, node capacity, and placement capacity failures map to `WORKLOAD_DIAGNOSTIC_CODE_ADMISSION_BLOCKED`; non-capacity placement failures map to node selection.

## Observability

Resource admission metrics are layered:

| Metric | Scope |
| --- | --- |
| `axern.controld_resource_admission_total{namespace,scope,result,reason}` | Shared resource admission decisions for Namespace quota and Node capacity. |
| `axern.controld_resource_admission_stage_duration_seconds{stage,result,error_class}` | Durable Namespace lock, candidate lock, Allocation-charge load, selection, and total admission latency. |
| `axern.controld_postgres_pool_connections{state}` | Current controld Postgres pool maximum, total, acquired, and idle connections. |
| `axern.controld_placement_selection_total{operation,result,mount_type}` | Placement selection attempts. |
| `axern.controld_placement_rejection_total{operation,result,reason}` | Placement candidate rejection reasons. |
| `axern.controld_namespace_resource_current{namespace,resource,state}` | Current namespace quota limits, used capacity, and available capacity. |
| `axern.controld_node_resource_current{resource,state}` | Current node CPU, memory, ephemeral-storage, and runtime-slot capacity, resource charge, and policy-derived resource state. |

Use admission stage durations and pool gauges to distinguish database-pool starvation from policy evaluation or locked admission work. The pool ceiling is a per-process deployment budget, so the sum across all controld replicas and auxiliary database clients must remain below the server connection limit.

The Namespace lock in resource admission is the linearizable quota boundary. Do not bypass it to reduce latency. Change this transaction shape only when repeated, same-contract load tests show the Namespace lock dominating the end-to-end Ready SLO after queue and database-pool waits have been ruled out.

### Repeatable Baseline

The admission correctness baseline is `TestConcurrentAdmissionDoesNotOversellNodeResources`. It starts twelve independent stores against one PostgreSQL database for each resource dimension and asserts the exact number of committed Allocations at the CPU, memory, ephemeral-storage, and runtime-slot boundary. `make controld-postgres-test` runs it against a freshly rebuilt PostgreSQL schema.

The pure policy and ordering hot paths have allocation-free Go benchmarks:

```bash
cd control/controld
go test ./internal/kernel/resource ./internal/kernel/placement \
  -run '^$' -bench 'Benchmark(AdmissionPolicyEvaluateFit|EvaluationLess)$' \
  -benchmem -count=3
```

On 2026-09-15, an Apple M4 Pro (`darwin/arm64`) measured `EvaluateFit` at 16.88–16.98 ns/op and `EvaluationLess` at 5.55–6.32 ns/op, both with zero allocations. These local numbers are regression references, not production SLOs. End-to-end P50/P95/P99 and lock wait must come from `controld_resource_admission_stage_duration_seconds`, node lifecycle RPC histograms, and axnoded startup phase histograms under the deployment workload; recording a workstation database latency as a portable target would be misleading.

The partial covering index `idx_allocations_resource_charge_node` serves Node charge scans without introducing a resource counter table. Namespace scans use `idx_runs_namespace_id` plus the unique Allocation `run_id`. Query-plan changes must preserve those facts and be measured against realistic non-released cardinality before adding an index or cache.

Public clients use the typed `diagnostic_code` directly instead of reclassifying raw backend messages:

| Surface Field | Values |
| --- | --- |
| `diagnostic_code` | `admission-blocked`, `node-selection-error`, or a concrete runtime diagnostic such as `runtime-start-error`. |
The human-readable Run `message` is operator context, not a machine contract. Typed rejection metadata separately identifies concrete resource insufficiency.

## Boundary Rules

- `internal/kernel/resource` owns pure policy, fit evaluation, and shared diagnostic reason names.
- `internal/placement` owns candidate selection and placement-specific `ErrorInfo` construction.
- `internal/postgres/resourceadmission` owns transactional Namespace quota and Node-capacity admission.
- `internal/application/run` maps admission failures into Run diagnostics. It does not sum or cache resource charges.
- `internal/kernel/resource` owns admission reason and diagnostic classification.
- CLI and SDK clients render the typed diagnostic emitted by the control plane and never infer it from message text.
- `axnoded` remains the runtime authority for cgroup enforcement; overcommit and quota do not change container limits.
- A nonzero `resources.limits.memory_bytes` automatically requires the selected runtime's typed memory-hard-limit capability. Its probes, evidence, and allocation-specific enforcement are defined by the canonical observed capability contract.
- `resources.requests.ephemeral_storage_bytes` participates in Namespace quota, placement, the Allocation charge, and axnoded's node-local enforcement record. `resources.limits.ephemeral_storage_bytes` is the runtime hard quota. Writable roots resolve missing limit to the configured default and missing request to the resolved limit; readonly roots reject nonzero ephemeral-storage resources.
- The charged scope is the sandbox-lifetime runsc file-backed root overlay, including metadata, copy-up, and whiteouts. It does not include immutable lowers or image caches, artifacts, projection placeholders, tmpfs, or logs.
- `requests.memory_bytes` is the Allocation's sandbox cgroup and Namespace-quota charge. `limits.memory_bytes` is the sandbox cgroup `memory.max`; runtime processes, guest accounting, shmem, kernel memory, lower and writable-overlay page cache, dirty pages, and writeback all share that boundary. There is no separate runsc overhead charge.
- Node-local control-plane processes are outside sandbox cgroups and are covered only by the Node's explicit `memory_system_reserve_bytes`. A terminal Run cannot make capacity reusable while its Allocation remains non-released. Node-local assigned and retiring commitments remain enforcement and cleanup diagnostics; they do not overwrite the central Allocation charge.
- `NodeSummary.capacity.memory_bytes` is the sole physical-capacity publication and `NodeMemoryBudget.source_allocatable_bytes` is the resource source's scheduling boundary. Scheduling caps that source by a finite delegated-root `memory.max`, subtracts the system reserve, and requires the result to equal the sole `NodeSummary.allocatable.memory_bytes` value. Node-local commitment, cleanup debt, and retiring cgroups are diagnostics and cannot alter central Allocation charges.
- The budget mode is explicit. Production `CGROUP_V2` observations require a positive system reserve and a boot/mount-scoped capacity identity. `DISABLED_DEV` still publishes resource-source capacity so local workloads can reserve memory, but uses zero reserve and cannot advertise or satisfy a runtime memory-hard-limit capability.
- Runtime pool exhaustion returned by `axnoded` is a runtime-start failure, not an admission block. Normal saturation must be rejected by transactional runtime-slot admission before node dispatch.
- Platform capability mismatch is a non-retryable eligibility failure. Only the explicitly classified transient-health cases may use lifecycle retry admission.
