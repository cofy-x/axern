---
title: Runs
description: Execute one-shot isolated commands from an image, template, or environment with durable records and real exit codes.
---

A Run is Axern's one-shot workload: it executes a command inside an isolated sandbox, streams output, propagates the command's exit code, and leaves a durable control-plane record. Detached Runs also provide the allocation lifecycle used by SDK Sandboxes and interactive tools.

## Run a command

```bash
axern run python:3.12-slim -- python -c 'print("hello from axern")'
```

The CLI attaches to stdout/stderr and exits with the remote command's real exit code. Every execution also creates a durable Run record:

```bash
axern run list
axern run get <run-id>
axern run logs <run-id> --follow
axern run cancel <run-id>
```

`run list` filters by `--namespace`, `--status` (`placed`, `starting`, `running`, `succeeded`, `failed`, `cancelled`), and `--label`. `run logs` supports `--follow` and resumable `--cursor` output; a single read is truncated at 64 MiB.

## Define a Run with a spec

Use a strict `axern/v1` spec when the definition should be reviewed or reused:

```yaml title="run.yaml"
api_version: axern/v1
kind: Run
metadata:
  namespace: default
  labels:
    example: docs
spec:
  source:
    image: docker.io/library/python:3.12-slim
  command:
    argv: [python, -c, "print('ok')"]
  resources:
    requests:
      cpu: 500m
      memory: 512Mi
  env:
    MODE: demo
```

```bash
axern run --file run.yaml
```

`spec.source` selects exactly one of `image`, `template` (with optional `template_version`), or `environment` (an existing environment ID). Private registries use `registry_credential_id`; credentials are referenced by ID and never embedded in the spec. The parser rejects unknown fields and conflicting sources.

The equivalent flags cover the same surface: `--env`, `--secret-env`, `--secret-file`, `--image-mount`, `--cwd`, `--label`, `--template`, `--environment`, and the four resource flags (`--request-cpu`, `--request-memory`, `--limit-cpu`, `--limit-memory`). `--file` cannot be combined with definition flags.

## Detached and long-running Runs

`--detach` creates the Run without following output; `--wait-timeout` bounds how long the CLI waits for the Run to become active (`0` disables the wait). Detaching does not detach the workload from the platform — the Run continues to a terminal state under the control plane and remains inspectable.

Run status is durable. Allocation-local stdout/stderr remain readable after runtime cleanup until `output_expires_at`, fixed at 15 minutes after cleanup begins. Node-process restart preserves sealed logs; node-disk loss does not. Output reads are limited to 64 MiB combined, with an explicit truncation signal. Download ordinary workspace files before cleanup; durable output publication belongs to the caller.

## Isolation and resources

Packaged nodes use `runsc` as the fixed isolation boundary. Resource requests and limits interact with namespace quota and admission; see [Runtime and Resources](/architecture/resources/) for the model and [Environments, Namespaces, and Quota](/guides/environments/) for inspecting admission rejections.

The CLI help is authoritative for the complete flag surface: `axern run --help`.
