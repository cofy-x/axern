# FunctionControl API

`axern.control.function.v1.FunctionControl` is the public API for function
bundle upload, deployment, revision lookup, invocation, invocation history,
and events.

## Ownership

- `Function`: stable namespace/name, active revision, desired spec, labels, and
  lifecycle state.
- `FunctionRevision`: immutable function spec and bundle source.
- `FunctionDeployment`: worker service, desired/ready replicas, scaling state,
  and diagnostics.
- `FunctionInvocation`: request identity, revision, payload metadata, result or
  structured error, duration, and terminal state.
- `FunctionEvent`: durable deployment, scaling, invocation, and cleanup facts.

`FunctionSpec.worker_source` selects either an existing environment ID or an
inline EnvironmentSpec. This source is persisted with each revision and is the
only source used to create or update the worker Service. Function runtime owns
the worker command, port, health endpoint, and invocation protocol; it does not
override the declared environment.

## RPC Surface

```text
UploadFunctionBundle
DeployFunction
GetFunction
ListFunctions
DeleteFunction
InvokeFunction
GetFunctionInvocation
ListFunctionInvocations
ListFunctionEvents
```

Bundle upload and deploy are separate so clients can stream deterministic
content before creating a revision. Invocation supports sync and async modes;
`request_id` provides idempotency. Function lookup accepts ID or
namespace/name. Invocation records remain queryable independently from generic
Run lifecycle.

Synchronous invocation remains bound to the caller request. Asynchronous
invocation is a durable PostgreSQL-backed queue: admission returns `QUEUED`, a
bounded controld dispatcher leases the immutable invocation revision, and
completion is fenced by lease token and execution generation. Expired leases
are reclaimable after a controld restart and invocation deadlines converge to
`TIMED_OUT` even when no dispatcher survives. PostgreSQL owns lease and deadline
time; worker dispatch receives only the remaining execution budget after queue
wait and worker preparation, and the completion transaction rejects results
that arrive after the durable deadline.

Asynchronous execution is at-least-once. The stable `invocation_id` and
`request_id` are forwarded to the handler so applications with external side
effects can deduplicate them. The platform does not claim exactly-once effects
across an HTTP response loss. A Function revision cannot be replaced or
deleted while it owns queued or running invocations; this prevents a worker
Service update from executing an older invocation with newer revision code.

All calls enter through gatewayd. Worker dispatch also uses gateway routing;
clients never receive node addresses or execution lease tokens. Generated Go
and Python contracts come from `sdk/proto/axern/control/function/v1`.
