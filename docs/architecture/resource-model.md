# Resource Model

Axern separates workload resource intent into three layers:

- request: scheduler and admission resource charge
- limit: runtime hard enforcement
- quota: namespace-level admission ceiling

This mirrors the common container platform model while keeping Axern's runtime boundary explicit: the control plane charges requests to the admitted Allocation; `axnoded` enforces limits through the `runsc` runtime; namespace quota caps admitted requests.

## Requests and Limits

`request` is the amount of CPU, sandbox memory, or ephemeral storage a workload asks Axern to reserve for placement and admission. Memory is the total sandbox cgroup budget, not a promise of guest-usable heap after runtime and page-cache usage. If a request is omitted, the control plane applies the default request before placement:

- CPU request: `500m`
- memory request: `4GiB`
- ephemeral-storage request for a writable rootfs: the resolved ephemeral-storage limit (default `256MiB`)

`limit` is the runtime hard cap for that resource. A memory limit becomes the host cgroup v2 `memory.max` for the complete sandbox domain, with swap disabled and group OOM enabled. Ephemeral-storage limits become runsc overlay size enforcement. A workload may set CPU or memory requests without matching limits; Axern still reserves capacity but does not install the omitted hard cap. Writable roots always resolve both an ephemeral-storage request and limit.

The invariant is:

```text
0 < request <= limit when a matching limit is set
```

CPU values accept cores or milli CPU:

```bash
--request-cpu 1
--request-cpu 500m
```

Memory values accept byte units:

```bash
--request-memory 512MiB
--request-memory 1GiB
--limit-memory 2GiB
```

Ephemeral-storage values use the same byte units. The resource means node-local, disposable storage managed by Axern for the lifetime of a sandbox. Writable roots require both an admitted request and a hard limit; readonly roots reject non-zero ephemeral-storage resources:

```bash
--request-ephemeral-storage 1GiB
--limit-ephemeral-storage 2GiB
```

Example:

```bash
axern run python:3.12-slim \
  --request-cpu 500m \
  --request-memory 512MiB \
  --limit-memory 1GiB \
  --request-ephemeral-storage 1GiB \
  --limit-ephemeral-storage 2GiB \
  -- python -c 'print("hello")'
```

Runs are the only workload owner for resource requests and limits. A Sandbox created by an SDK uses the same Run/Allocation charge and enforcement path; there is no parallel Service resource model.

## Node Admission

Node admission uses workload requests, not runtime limits. The control plane checks candidate nodes twice:

1. the in-memory placement pass filters immutable requirements, freshness, readiness, and requests that can never fit the observed total allocatable capacity
2. the Postgres admission transaction locks candidate Node rows and checks current resource charges from non-released Allocations before committing

Only the second check decides remaining capacity. Node runtime usage is pressure telemetry, not a second charge ledger.

The authoritative transaction also reruns lifecycle, freshness, runtime, component, label, typed-capability, capacity, and slot eligibility. Capability requirements and the immutable node binding commit atomically with the Allocation resource charge; node observations are not copied into a second admission record. See the [Observed Capability Providers](observed-capability-providers.md) contract.

CPU can be overcommitted globally by `controld` with `-resource-cpu-overcommit-ratio`. The effective CPU allocatable value is:

```text
floor(node_allocatable_cpu_milli * resource_cpu_overcommit_ratio)
```

Memory and ephemeral storage are not overcommitted. Their allocatable values come from the current Node observation after the relevant system reserves.

The binding, immutable requests, Run, and ensure-present intent commit together. CPU, memory, and ephemeral-storage charge therefore has no independent row: it is the sum of request columns on Allocations whose lifecycle is not `RELEASED`. Runtime-slot occupancy additionally unions those Allocation IDs with node-reported active Allocation IDs, so an orphan or asynchronously deleting sandbox cannot make a discrete slot reusable early.

## Ephemeral Storage Accounting Scope

The current charged scope is intentionally narrow and runtime-independent:

- the runsc file-backed root overlay, including its metadata, copy-up, and whiteouts

Immutable lower rootfs and image caches, mount-target projection placeholders, tmpfs, logs, and process output streams are not charged to `ephemeral_storage_bytes`. Extending the charged scope requires an explicit contract change; it must not silently consume an existing Allocation charge.

Allocation-local files are not a persistent volume contract. Applications must export data that needs to survive allocation replacement or node loss; see the [storage ownership and lifetime model](storage-architecture.md).

Overcommit changes control-plane admission capacity only. It does not change container cgroup limits or runtime behavior.

## Namespace Quota

Namespace quota is a control-plane admission ceiling over active workload requests in a namespace. It limits the CPU, memory, and ephemeral-storage charges held by non-released Allocations, independent of which node runs each workload.

Omitted quota fields are unlimited. `quota unset` returns the namespace policy to unlimited CPU, memory, and ephemeral storage. Existing admitted workloads keep running if quota is lowered below current usage; future admissions are blocked until usage falls back under the new limit.

Typical namespace quota flow:

```bash
axern namespace create team-a
axern quota set --namespace team-a --cpu 4 --memory 32GiB
axern quota get --namespace team-a
axern quota unset --namespace team-a
axern namespace delete team-a
```

Quota usage is based on Allocation resource charges until cleanup reaches `RELEASED`. A terminal Run continues to consume capacity while node cleanup is incomplete, preventing premature reuse of resources that may still be live.

Quota and node admission are separate gates:

- quota answers whether the namespace may admit more requested resources
- node admission answers whether an eligible node has remaining effective request capacity
- CPU overcommit changes node admission capacity only; it does not increase namespace quota
- memory is strict for both namespace quota and node admission
- runtime memory is already inside the sandbox budget and is never added as a hidden overhead charge
- node-local daemons are outside sandbox cgroups and consume the separately qualified node system reserve, which is not namespace quota usage

Namespace deletion is lifecycle cleanup, not quota reset. It rejects live operational state such as non-released Allocations, non-terminal Runs, live Environments, or Secrets. Historical terminal workload metadata can keep its namespace string for auditability without blocking deletion.

## Diagnostics

Resource admission failures are surfaced in CLI and SDK responses with stable diagnostic labels.

Plain `no eligible node` failures that do not contain capacity rejection details are node-selection failures, not resource admission failures. Examples include stale node state or missing node capabilities.

For machine-readable troubleshooting, use JSON output:

```bash
axern run get <run_id> -o json
```

Resource-related Run failures expose `diagnostic_code` as the stable machine field and `message` as operator context. Clients must not reconstruct diagnostic categories by parsing that human-readable message. Typed placement rejection details distinguish the concrete exhausted resource when callers need finer-grained admission analysis.

Run creation returns admission failures directly. Detached SDK Sandboxes expose later allocation failures through the owning Run status and diagnostics.

## Related Implementation Docs

- [Control-plane resource admission](../../control/controld/docs/resource-admission.md)
- [Control-plane namespace quota](../../control/controld/docs/resource-quota.md)
- [Node runtime resource handling](../../runtime/axnoded/docs/resource.md)
- [Observed capability providers](observed-capability-providers.md)
