# External Runner Integration

Axern is an execution platform, not an evaluation orchestrator. An external runner such as `cofy-x/axrun`, mini-swe-agent, OpenHands, or a benchmark harness owns episodes, agent policy, inference, CandidateBundle schemas, verifier semantics, trajectories, scoring, datasets, and long-term result storage. It consumes only a released Axern SDK.

## Public Execution Flow

The recoverable path is:

```text
resolved task input
  -> Environment
  -> inference Run -> Allocation -> runsc sandbox
  -> sealed declared CandidateBundle bytes
  -> fresh verification Run -> new Allocation -> new runsc sandbox
  -> sealed declared VerificationResult bytes
  -> caller-owned durable publication
```

The runner persists the public `environment_id`, `run_id`, and `allocation_id`. It never supplies or stores a Node ID, node target, runtime/container ID, access-grant token, ExecutionLease, watch cursor, OCI label, or node-local path. Gatewayd resolves the Allocation binding and issues purpose-scoped access internally.

Inference and verification are separate Runs and Allocations. A verifier receives the downloaded candidate through the public file or archive API; it never reuses the inference writable filesystem. CandidateBundle and VerificationResult schemas remain caller-owned bytes and media types rather than Axern product objects.

Tool/runtime bundles may be attached with immutable read-only image mounts. Secret projections are explicit inputs for ordinary workload credentials that truly must exist inside a sandbox. A fresh verifier Run must declare its own mounts and Secret references and never inherits them from the inference Environment or Run. The public SDK supplies only image references, mount targets, Secret IDs, keys, and projection targets; it never supplies Node identity, runtime identity, or plaintext Secret values.

Claude Code runtime images are digest-pinned and mounted read-only at the canonical `/__claude_code` target. This is an ordinary `ImageMount` in the inference Run, not a new agent product object, and it is absent from the fresh verifier unless that verifier explicitly declares its own mount.

Model-provider identity belongs to the external runner, not the sandbox. The recommended model path is `Allocation -> TunnelSession -> runner-local loopback model gateway -> provider`. The runner retains the Axern mTLS private key, Provider API key or client certificate, routing policy, budget and protocol adaptation. Only the Allocation-local TCP endpoint enters the sandbox; the Tunnel client token and real Provider identity do not. Revoking or expiring the Tunnel closes model access without extending or terminating the Run. A TunnelSession never enters CandidateBundle or VerificationResult bytes and must not be reused by the fresh verifier.

## Declared Outputs

Declare outputs in the immutable Run specification before execution. Each declaration contains an absolute sandbox path, `file` or `tar` format, and optional media type. At cleanup, axnoded quiesces the Allocation and atomically seals stdout, stderr, and declared objects before runtime deletion.

Current limits are:

- at most 16 declared paths per Run;
- 64 MiB per file;
- 256 MiB per tar archive;
- 256 MiB combined declared-output bytes per Allocation;
- 15 minutes of node-local retention after cleanup begins.

The manifest reports available, missing, rejected, capture-failed, or node-unavailable status with a reason. Available objects include an opaque object ID, size, SHA-256 digest, media type, format, seal time, and expiry. The SDK verifies size and digest during a complete download. A digest is integrity evidence, not object identity.

Sealed output survives runtime deletion, client restart, and axnoded process restart. It does not survive node-disk loss and is not replicated by Axern. A caller must copy accepted bytes to its own durable store before expiry. Output access is read-only and cannot revive a terminal Allocation or grant process/file mutation authority.

## Process And Connection Semantics

`exec` is a bounded convenience API. Collected stdout and stderr are limited to 1 MiB per stream in the Python SDK; truncation is explicit and callers with larger output use `exec_stream` or `process`. TypeScript pauses the gRPC read stream at 64 queued events and resumes after the queue drains. Python bounds outgoing attached-process requests at 64 entries and rejects writes when the caller outruns the stream. Go exposes incremental receive APIs.

An attached process belongs to one Allocation and one live stream. `close` attempts `TERM` before closing the stream; explicit `kill` uses `KILL`. Disconnect does not create a durable process identity. Run cancellation and Allocation cleanup remain the only durable workload termination path.

SSH and Terminal are diagnostic and interactive access. Tunnel is also the generic Allocation-scoped reverse-TCP primitive used for a runner-owned loopback model gateway. None of their connection state is an episode, candidate, verifier result, or durable Run result. Revoking access closes or prevents data-plane connections but does not by itself redefine the Run lifecycle. Tunnel peers are revalidated every 15 seconds with a 5-second deadline, so the online revocation and control-loss bound is 20 seconds excluding process scheduling pauses; Axern does not promise zero-delay revocation.

## Recovery

After a client restart:

1. reconstruct the SDK client from its explicit endpoint and credential;
2. call `get_run(run_id)` and inspect terminal status, exit code, diagnostic, and `allocation_id`;
3. use `allocation_id` only for live Allocation-scoped operations;
4. query the sealed-output manifest by `run_id`;
5. download and verify available objects before expiry;
6. create a new Run for verification or retry.

If the manifest is not yet available immediately after the Run becomes terminal, retry `NOT_FOUND` within a bounded deadline: Run terminal commit and node cleanup converge across processes. Permission denial, expiry, rejected output, integrity mismatch, and node unavailability are terminal for that download attempt and must not be hidden by an unbounded retry.

## Package Boundary

Released Python, TypeScript, and Go SDK artifacts include public control, Run, Tunnel, and NodeSandbox contracts only. Gateway routing, plaintext access grants, Node enrollment/control, lifecycle delivery, operator/debug, egress, network resolver, and relay-control Protocol Buffers are repository-private and absent from SDK packages. External projects must not import Axern internal packages or use a source checkout, local path, or Go `replace` directive.

Released SDK artifacts must install into source-free consumers and provide the complete public execution flow without local paths, source checkouts, internal packages, or workspace replacements.
