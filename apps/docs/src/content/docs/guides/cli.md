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
axern catalog list
```

For a Helm installation, keep a gateway port-forward open and import the
chart-generated mTLS identity:

```bash
kubectl --namespace axern-system port-forward svc/gatewayd \
  25100:25000 25101:25080 25122:25022

axern context import-kubernetes local \
  --namespace axern-system \
  --current
```

## Run isolated Python

```bash
axern run create \
  --namespace default \
  --template-id python311 \
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
    template: python311
  command:
    argv: [python, -c, "print('ok')"]
  runtime_class: runsc
  resources: {}
```

```bash
axern run create --file run.yaml --wait --output json
```

The CLI help is authoritative for the complete flag surface. See the
[CLI source guide](https://github.com/cofy-x/axern/tree/main/apps/cli) for
contexts, exit codes, aliases, services, tunnels, and admin workflows.

![Axern CLI command overview](/terminal/axern.gif)
