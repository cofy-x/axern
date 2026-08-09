# Resource Model

Axern separates workload resource intent into three layers:

- request: scheduler and admission reservation
- limit: runtime hard enforcement
- quota: namespace-level admission ceiling

This mirrors the common container platform model while keeping Axern's runtime
boundary explicit: the control plane admits and reserves requests; `axnoded`
enforces limits through the selected `runsc` or `runc` runtime; namespace quota
caps admitted requests.

## Requests and Limits

`request` is the amount of CPU, memory, or ephemeral storage a workload asks Axern to reserve for
placement and admission. If a request is omitted, the control plane applies the
default request before placement:

- CPU request: `500m`
- memory request: `4GiB`
- ephemeral-storage request for a writable rootfs: the resolved
  ephemeral-storage limit (default `256MiB`)

`limit` is the runtime hard cap for that resource. CPU and memory limits become
Linux cgroup settings. Ephemeral-storage limits become runsc overlay size or
runc XFS project-quota enforcement. A workload may set CPU or memory requests
without matching limits; Axern still reserves capacity but does not install the
omitted hard cap. Writable roots always resolve both an ephemeral-storage
request and limit.

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

Ephemeral-storage values use the same byte units. The resource means
node-local, disposable storage managed by Axern for the lifetime of a sandbox.
Writable roots require both a reservation and a hard limit; readonly roots
reject non-zero ephemeral-storage resources:

```bash
--request-ephemeral-storage 1GiB
--limit-ephemeral-storage 2GiB
```

Example:

```bash
axern run --template python311 \
  --request-cpu 500m \
  --request-memory 512MiB \
  --limit-memory 1GiB \
  --request-ephemeral-storage 1GiB \
  --limit-ephemeral-storage 2GiB \
  -- python -c 'print("hello")'
```

Runs and services use the same resource flags. Mutable service resource
settings can be changed with `axern svc update`; omitted resource flags are
unchanged.

## Node Admission

Node admission uses workload requests, not runtime limits. The control plane
checks candidate nodes twice:

1. placement prefilter selects nodes whose observed resources appear to fit
2. the Postgres reservation transaction locks the selected node and rechecks
   active reservations before committing

Both checks use the same resource admission policy.

The authoritative transaction also reruns lifecycle, freshness, runtime,
component, label, typed-capability, capacity, and slot eligibility. Capability
requirements and selected evidence commit atomically with the reservation; see
the [Observed Capability Providers](observed-capability-providers.md) contract.

CPU can be overcommitted globally by `controld` with
`-resource-cpu-overcommit-ratio`. The effective CPU allocatable value is:

```text
floor(node_allocatable_cpu_milli * resource_cpu_overcommit_ratio)
```

Memory and ephemeral storage are not overcommitted. Their effective allocatable
values come from the node inventory after the relevant system reserves.

## Ephemeral Storage Accounting Scope

The current charged scope is intentionally narrow and runtime-independent:

- the runc sandbox-private writable rootfs upper, including copy-up, metadata,
  and whiteouts
- the runsc file-backed root overlay, including its metadata, copy-up, and
  whiteouts

Persistent volumes, immutable lower rootfs and image caches, artifacts,
mount-target projection placeholders, tmpfs, and logs are not charged to
`ephemeral_storage_bytes` in the current contract. Adding one of those classes
later requires an explicit accounting-version change; it must not silently
consume an existing sandbox reservation.

Overcommit changes control-plane admission capacity only. It does not change
container cgroup limits or runtime behavior.

## Namespace Quota

Namespace quota is a control-plane admission ceiling over active workload
requests in a namespace. It limits the CPU, memory, and ephemeral-storage
reservations a namespace can hold, independent of which node eventually runs
each workload.

Omitted quota fields are unlimited. `quota unset` returns the namespace policy
to unlimited CPU, memory, and ephemeral storage. Existing admitted workloads keep running if quota
is lowered below current usage; future admissions are blocked until usage falls
back under the new limit.

Typical namespace quota flow:

```bash
axern namespace create team-a
axern quota set --namespace team-a --cpu 4 --memory 32GiB
axern quota get --namespace team-a
axern quota unset --namespace team-a
axern namespace delete team-a
```

Quota usage is based on active workload reservations. A completed, cancelled, or
released workload no longer counts against namespace quota.

Quota and node admission are separate gates:

- quota answers whether the namespace may reserve more requested resources
- node admission answers whether an eligible node has remaining effective
  request capacity
- CPU overcommit changes node admission capacity only; it does not increase
  namespace quota
- memory is strict for both namespace quota and node admission

Namespace deletion is lifecycle cleanup, not quota reset. It rejects live
operational state such as active reservations, non-terminal runs, live
environments, live services, or secrets. Historical terminal workload and
Function invocation metadata can keep their namespace string for auditability
without blocking deletion.

## Diagnostics

Resource admission failures are surfaced in CLI output, JSON output, and the
local dashboard with stable diagnostic labels.

Plain `no eligible node` failures that do not contain capacity rejection details
are node-selection failures, not resource admission failures. Examples include
unsupported runtime classes, stale node state, or missing node capabilities.

For machine-readable troubleshooting, use JSON output:

```bash
axern svc get <service_id> -o json
axern run get <run_id> -o json
```

Resource-related service failures expose `diagnostic_code` as the stable machine
field and `admission_summary` as the compact operator-facing label:

| Summary | Meaning |
| --- | --- |
| `namespace quota exceeded` | Namespace quota blocked the requested reservation. |
| `node CPU capacity exhausted` | Otherwise eligible nodes lack effective CPU request capacity. |
| `node memory capacity exhausted` | Otherwise eligible nodes lack memory request capacity. |
| `node CPU and memory capacity exhausted` | Otherwise eligible nodes lack both CPU and memory request capacity. |
| `node reservation capacity exhausted` | Placement found candidates, but transaction-time reservation recheck found no remaining capacity. |

The typed placement rejection set distinguishes insufficient ephemeral storage
even though the current compact CLI summary may use the generic reservation or
capacity label.

Run creation returns admission failures directly. Service creation and update
accept desired state first; later replica admission failures surface through
service status, events, dashboard DTOs, and JSON output.

## Related Implementation Docs

- [Control-plane resource admission](../../control/controld/docs/resource-admission.md)
- [Control-plane namespace quota](../../control/controld/docs/resource-quota.md)
- [Node runtime resource handling](../../runtime/axnoded/docs/resource.md)
- [Observed capability providers](observed-capability-providers.md)
