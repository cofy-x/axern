---
title: Functions
description: Deploy named Python functions with immutable revisions, warm workers, and invocation history.
---

Function is Axern's model for repeated event handling. A Function owns a
stable name, immutable revisions, worker scaling, and a durable invocation
history. Interactive process, file, browser, and computer-use workflows use a
Sandbox instead.

## Define a function

A Function spec selects the worker environment at `spec.source` and packages a
local source directory as the immutable function bundle:

```yaml
api_version: axern/v1
kind: Function
metadata:
  name: hello
  namespace: default
spec:
  source:
    template: python311
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

The handler receives the invocation event and a context with environment,
request ID, and initializer state:

```python
def init(context):
    return {"initialized": True, "function": context.function_name}


def hello(event, context):
    name = event.get("name", "world")
    greeting = context.env.get("GREETING", "hello")
    return {
        "message": f"{greeting} {name}",
        "request_id": context.request_id,
        "state": context.state,
    }
```

The parser rejects unknown fields, conflicting sources, unsafe source paths,
and invalid scaling. Credentials are referenced by ID and never embedded in
the spec.

The manifest resolves `function.source` relative to the manifest directory.
A minimal repository layout is:

```text
hello/
  function.yaml
  payload.json
  src/
    handler.py
```

With `source: src` and `handler: handler.hello`, the bundle must contain
`src/handler.py`. Keep `payload.json` outside the bundle; it is the invocation
input. The handler return value is serialized as the invocation result, while
an exception produces a failed invocation. Initializer state is reused by a
warm worker and must not be treated as durable storage.

## Deploy and invoke

```bash
axern function deploy --file function.yaml --wait
axern function get --namespace default hello

axern function invoke --namespace default hello -d '{"name":"axern"}'
axern function invoke --namespace default hello --payload-file payload.json
axern function invoke --namespace default hello -d '{"name":"async"}' --async

axern function invocation list --namespace default hello
axern function invocation get <invocation-id>
axern function delete --namespace default hello
```

Async invocation is durably queued and reaches a terminal state independently
of the client connection. Delivery is at-least-once after a dispatcher
failure, so handlers should use the stable invocation ID to deduplicate
external effects. Revision replacement and deletion wait for active
invocations to finish.

## Deploy from Python

```python
from axern_sdk import AxernClient, Function

client = AxernClient.from_context("~/.config/axern/config.json")
fn = Function.from_file(client, "function.yaml")

fn.deploy(wait_ready=True)
result = fn.invoke({"name": "axern"})
print(result.value)   # handler return value
print(result.status)  # "succeeded"
```

`Function.deploy()` packages the source into a deterministic bundle, uploads
it, and creates the revision; `invoke()` uses the dedicated invocation API
rather than a generic Run wrapper.

A complete source directory lives in the repository's
[function-hello example](https://github.com/cofy-x/axern/tree/main/examples/function-hello);
the [function user model](https://github.com/cofy-x/axern/blob/main/docs/product/function-user-model.md)
is the authoritative contract.
