# Durable rollout control plane

Axern's rollout control plane turns an immutable Axrun TaskSet into a
restart-safe queue of planning and episode work. It is intentionally narrower
than a generic workflow engine: the aggregate is a rollout, the unit of
scheduling is an episode attempt, and the result is reproducible evidence.

## Ownership and state

- `controld` owns public rollout/profile APIs, lifecycle, scheduling policy,
  leases, retries, frozen snapshots, budgets, usage accounting, diagnosis,
  artifact metadata, and download-ticket authority.
- PostgreSQL is authoritative. Each `controld` replica uses one dedicated
  session to `LISTEN` for both rollout event and work changes, then fans bounded
  wakeups out to local event streams and worker long-polls without consuming
  the query pool. Candidate-work hints select one compatible FIFO waiter per
  replica; capacity-release hints select at most one waiter per capability
  group, and lease renewal does not notify. Candidates may have a future
  `next_run_at`, so notifications reduce latency but
  are never required for correctness; streams resume from durable sequence
  numbers and workers always reclaim authoritative rows transactionally after
  a hint or jittered safety timeout.
- `axrun worker` owns TaskSet planning, the real provider/model preflight probe,
  and episode execution through Axern. It has no authority to mutate arbitrary
  rollout state. Controld never performs a provider HTTP request.
- S3-compatible storage owns evidence bytes. PostgreSQL stores their digest,
  size, generation, status, and object key.

The public `RolloutControl` and `AgentProfileControl` APIs are available through
gatewayd. `RolloutWorkerControl` is registered only when a bootstrap token is
configured and is deliberately absent from the HTTP/gateway allowlist. Workers
connect directly to controld over mTLS for registration, claims, lease renewal,
profile resolution, metering, and artifact metadata. Allocation and sandbox
execution use a separate mTLS context through gatewayd. This keeps the private
worker authority off the external edge while preserving the normal gateway
routing path for public execution APIs; controld does not proxy sandbox traffic.

## Fencing and convergence

Claiming work atomically changes it from `PENDING` to `LEASED`, assigns a
cryptographically random token hash, and increments the attempt counter. Every
progress, profile resolution, usage, artifact, and completion call must match
the active token and unexpired lease. Episode retries increment
`execution_generation`; artifact paths and result updates include that
generation, fencing stale workers.

Managed provider probes and episodes always create durable usage reservations,
including unlimited rollouts. Completion names the exact committed reservation
and controld rejects a usage mismatch. Aggregate usage is computed from every
committed reservation, so real provider calls made before an infrastructure
retry are not hidden by the final episode attempt.

`ArtifactAccess` shares controld's internal listener but is not authorized by a
generic platform client certificate. The handler requires the verified
`gatewayd` certificate identity before returning an internal presigned request;
the public edge and rollout clients can only receive streamed artifact bytes.

Cancellation marks active work and converges the rollout after workers observe
it or leases expire. Infrastructure failures may be retried; agent and verifier
outcomes remain evidence, not infrastructure retries. Completion is idempotent
for the original lease/result and rejects conflicting late results.

Manual planning creates HELD episode work and reaches `READY`; it cannot be
claimed until `StartRollout` atomically changes the frozen plan to queued work.
Auto-start rollouts queue the same frozen plan directly. Start is idempotent
after the durable transition: later calls return the already-started Rollout
without reopening or changing the frozen plan. The first accepted operation key
is retained for audit and client retry correlation.

## Budgets and credentials

Agent Profiles own hidden immutable encrypted credential versions. Generic
Secret get/list cannot expose them. Create/rotate and Profile pointer updates
are transactional and version checked. Rollout admission freezes the Profile
spec, Profile version, credential version, and hidden reference; READY and
running rollouts therefore survive later rotation without changing identity.
Plaintext is returned only to a worker holding the matching plan, doctor, or
episode lease and never enters public protobufs, events, logs, or artifacts.
Profile concurrency covers planning probes, doctor probes, and episodes, and is
scheduled by frozen Profile ID and version. A later
update or rotation therefore creates a new concurrency group and cannot alter
the limit observed by READY or running Rollouts from an older snapshot.

The planning worker probes the provider with at most one output token. Probe
usage is committed to the rollout budget; missing provider usage is charged at
the bounded estimate. Test pricing is injected only into local worker test
infrastructure and is not part of the production price table.

Before episode execution, workers reserve a bounded token and micro-USD allowance in a
transaction. Commit converts the reservation to actual usage and release makes
unused allowance available. Concurrent workers therefore cannot each observe
the same remaining budget. Wall-time expiry is reconciled independently of
worker health.

## Evidence lifecycle

Workers upload canonical task, episode, trajectory, agent, verifier, and reward
records using short-lived presigned PUTs. The checksum and Axern digest metadata
are signed; controld checks object size and checksum/digest metadata before
marking an artifact present. An evidence manifest, uploaded last, is stored on
the episode as the root artifact ID.

Deleting a terminal rollout first transitions it to `DELETING`. A reconciler
removes the rollout's object prefix and only then deletes the PostgreSQL
aggregate. Object-store failures leave durable state available for retry.

Clients never receive those internal object-store requests. Public
`PrepareArtifactDownload` returns metadata and a short-lived HMAC ticket bound
to artifact ID, generation, digest, size, expiry, and audience. Gatewayd's
`ArtifactData.Download` resolves it over private mTLS and streams fixed-size
chunks. Axrun resumes into an adjacent `.part`, renews expired tickets, verifies
exact size and SHA-256, and atomically installs the final file.
