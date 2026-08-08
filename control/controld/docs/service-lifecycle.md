# Service Lifecycle

Services are the long-running, replica-oriented workload model in `controld`.
They can be gateway-routed, health checked, rolled forward, and autoscaled.
`Run` does not consume service rollout or autoscaling policy. A Function owns a
Service worker, so its execution capacity follows the same rollout contract.

## Probes

Services can declare `readiness_probe` and `liveness_probe`.

- `readiness_probe` gates `ready_replicas` accounting and rollout progress.
  A service is only `READY` when the desired replicas are both `RUNNING` and
  readiness-confirmed.
- `liveness_probe` failures are treated as ended unhealthy replica failures.
  They flow through service replacement instead of a node-local restart loop.

## Rollout

Service rollout is driven by desired-state drift between the service spec and
current allocations. Changes to `environment_id`, execution `config`,
`readiness_probe`, or `liveness_probe` trigger rolling replacement.

Admission stores a deterministic `desired_spec_digest` on every allocation.
The digest covers exactly those normalized execution-intent fields and excludes
replica count, rollout progress, status, and other mutable service state.
Rollout comparison uses this identity rather than attempting to reverse
rootfs-dependent defaults from the resolved allocation config. Allocations
created before the identity exists have an empty digest and are replaced once
under the normal rollout availability budget.

Replacement admission respects `max_surge`. Outdated draining respects
`max_unavailable` and readiness, so an updated replica must become ready before
an old ready replica is removed when the availability budget requires it.

## Allocation Lifecycle Retry

Service allocation admission is durable before `controld` calls the selected
node. The Postgres transaction owns the service allocation row, node
reservation, namespace quota reservation, tunnel target state, and execution
lease state. Node lifecycle calls happen after commit and use the shared
`allocation_reconcile_queue` described in
[Node Placement and Leases](./node-placement-and-leases.md).

Create retry covers node create failures after admission has committed. The
queue uses exponential backoff starting at 2 seconds, capped at 30 seconds, and
fails the allocation after 5 create attempts. Exhausting create retry releases
the reservation, revokes the lease, records the failed replica, and lets service
reconciliation admit a replacement according to the rollout policy.

Delete retry covers node delete failures. It retries every 5 seconds until the
node confirms deletion, then releases the reservation, revokes the lease, and
clears the queue item. Delete retry is intentionally not capped because cleanup
must keep converging until node-local work is gone.

`DeleteService` persists the deleted desired state and wakes reconciliation; it
does not synchronously drain every replica before acknowledging the RPC. This
keeps delete latency independent of replica count and leaves node cleanup under
the same durable retry contract as every other lifecycle transition. Service
reconciliation atomically marks each allocation `RELEASING` and enqueues its
delete work; bounded allocation workers perform node deletion and confirmation
in parallel across nodes.
`PurgeService` is a pure persistence finalizer: it succeeds only after every
allocation has reached `RELEASED`. It never calls a node or retries allocation
deletion. The service reconciler is the only owner of allocation release and
node deletion confirmation.

`ServiceReplica.lifecycle_retry` exposes the current retry reason, attempts,
last error, and next retry time for operators and SDK clients. Ready replicas on
the normal path should not have lifecycle retry state. `controld` also exports
queue count, oldest queue age, and max attempt metrics so dashboards can surface
stuck lifecycle work without inspecting Postgres directly.

Run and service both use the same durable queue, but service retry feeds back
into rollout state. A failed service create retry becomes an ended replica and
opens capacity for a replacement; a successful delete retry is a purge
precondition.

## Autoscaling

Service autoscaling is deliberately lightweight in V1. A policy can define
`min_replicas`, `max_replicas`, and UTC cron schedules. Matching schedules set
the effective desired replica count. When no schedule is active, the service's
manual `replicas` value remains the desired count.

V1 does not implement CPU, QPS, custom metric autoscaling, cooldown windows, or
metrics ingestion.

## Volumes

Service execution configs can declare V1 node-local `volume_mounts`. `controld`
stores the logical volume intent with the service config and sends it to the
selected node as resolved local `ResolvedNodeVolume` specs.

The field is rejected for run workloads in V1. Node-local volumes
persist on the selected node only and do not imply cross-node migration,
replication, backup, quota, or garbage collection behavior.

The next storage phase is tracked in [`../../storaged`](../../storaged/README.md).
That design moves long-term storage ownership toward `VolumeClass`,
`VolumeClaim`, and `VolumeBinding` contracts while preserving the current
node-local volume behavior as the first backend.
