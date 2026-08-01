---
title: Axern CLI
description: Manage contexts and run isolated workloads through the Axern public API.
---

`axern` is the product CLI for resources and interactive development. It talks
to public APIs through `gatewayd`; it never connects directly to node or
database internals.

## Confirm the current context

```bash
axern context list
axern context current
```

For a Helm installation, keep a gateway port-forward open and import the
chart-generated mTLS identity. SSH is optional and disabled by the default
chart values, so the basic CLI path only forwards the control and HTTP ports:

```bash
kubectl --namespace axern-system port-forward svc/gatewayd \
  25100:25000 25101:25080

axern context import-kubernetes local \
  --namespace axern-system \
  --endpoint 127.0.0.1:25100 \
  --service-url http://127.0.0.1:25101 \
  --ssh-endpoint "" \
  --current
```

Use the [Kubernetes install guide](/getting-started/kubernetes/) when an
interactive agent workflow also needs the separately enabled SSH port.

## Diagnose the platform

Start with the read-only platform doctor. It validates the selected context,
mTLS certificate lifetime and key permissions, gateway connectivity, namespace
access, and the runtime catalog without creating resources:

```bash
axern doctor --namespace default
```

Use an explicit probe when control-plane reachability is not enough:

```bash
axern doctor --namespace default --probe
```

The probe creates a temporary catalog-backed Environment from the `python311`
template, executes a small `runsc` Run, and deletes the Environment after the
Run reaches a terminal state. The Run remains as normal control-plane history.
JSON output exposes stable check codes without printing certificate paths,
private keys, raw endpoints, or server error text. Doctor exits with `0` for
healthy, `1` for degraded, `2` for invalid usage or connection configuration,
and `3` when a required platform health check fails.

Use `axern identity whoami` to inspect the Principal, active certificate, and
effective roles for the selected context. Platform administrators can manage
durable Principals and namespace bindings with `axern admin principal`,
`axern admin credential`, and `axern admin role-binding`. See
[Identity and namespace access](/guides/authorization/) for the least-privilege
workflow.

## Run isolated Python

```bash
axern run create \
  --namespace default \
  --image-ref docker.io/library/python:3.12-slim \
  --runtime-class runsc \
  --argv python \
  --argv -c \
  --argv 'import platform; print(platform.python_version())' \
  --wait
```

Use a strict resource file when a specification should be reviewed or reused:

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
  runtime_class: runsc
  resources: {}
```

```bash
axern run create --file run.yaml --wait --output json
```

OCI images are the portable default for new workloads. Use
`axern catalog list` and `--template-id` when the platform provides a named,
reusable environment with a curated toolchain or configuration.

## Pass credentials without putting values in argv

Prefer stdin for provider tokens and opaque secrets. These forms keep the
value out of command arguments. Referencing an existing environment variable
also avoids writing the value literally in the command or heredoc history, but
the variable remains visible to the local CLI process:

```bash
printf '%s\n' "$OPENAI_API_KEY" | axern agent profile set dev-codex \
  --agent codex \
  --provider openai \
  --upstream https://api.openai.com/v1 \
  --token-stdin \
  --model <model>

printf '%s\n' "API_KEY=$AXERN_SECRET_API_KEY" | \
  axern secret create --namespace default --literal-stdin
```

`--token-env NAME` is also available for agent profiles. The CLI deliberately
does not accept provider tokens or opaque secret values as command-line
arguments.

The CLI help is authoritative for the complete flag surface. See the
[CLI source guide](https://github.com/cofy-x/axern/tree/main/apps/cli) for
contexts, exit codes, aliases, services, tunnels, and admin workflows.

![Axern CLI command overview](/terminal/axern.gif)
