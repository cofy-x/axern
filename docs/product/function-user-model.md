# Function User Model

Function is Axern's product model for repeated event handling. It owns a stable
name, immutable revisions, worker scaling, invocation results, and invocation
history. Interactive process, file, browser, and computer-use workflows use
Sandbox instead.

## Resource Spec

```yaml
api_version: axern/v1
kind: Function
metadata:
  name: hello
  namespace: default
  labels:
    app: hello
spec:
  source:
    template: python311
  resources:
    requests: {cpu: 600m, memory: 512MiB}
    limits: {cpu: "1", memory: 1GiB}
  env:
    GREETING: hello
  function:
    runtime: python3.11
    handler: handler.hello
    initializer: handler.init
    source: src
    timeout_seconds: 600
    scaling:
      min_replicas: 0
      max_replicas: 10
      concurrency: 2
      idle_timeout: 5m
```

Top-level `spec.source` selects the worker environment: an existing environment,
catalog template, or OCI image. `spec.function.source` is a relative local
directory packaged as the immutable function bundle. The two fields have
different ownership and both are required.

The parser rejects unknown fields, source conflicts, invalid resource units,
unsafe source paths, invalid scaling, and unsupported runtime contracts.
Credentials are referenced by ID and never embedded in the spec.

## Commands

```bash
axern function deploy --file function.yaml --wait
axern function get --namespace default hello
axern function invoke --namespace default hello --data '{"name":"axern"}'
axern function invocation list --namespace default hello
axern function invocation get <invocation-id>
axern function invocation events <invocation-id>
axern function delete --namespace default hello
```

## Runtime Contract

Controld stores the Function and revision, resolves the declared worker
environment, and reconciles a Function-owned Service. Runtime selects the
worker command and protocol contract; it does not silently select the worker
image. The worker downloads the immutable bundle, initializes the handler,
serves health and invocation endpoints, and returns structured results.

Function invocation is a dedicated API and history model. It is not a generic
Run wrapper and does not expose allocation IDs, node targets, or execution
leases as its user contract.

Async invocation is durably queued and reaches a terminal invocation state
independently of the client connection. Delivery is at-least-once after a
dispatcher failure; handlers use the stable invocation ID for external-effect
deduplication. Revision replacement and Function deletion wait for active
invocations to finish instead of silently moving them onto different code.

See [Function Hello](../../examples/function-hello/README.md) for a complete source
directory and [FunctionControl](./function-control-proto-design.md) for the API
boundary.
