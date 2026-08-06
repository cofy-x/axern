---
title: Services
description: Run long-lived HTTP workloads with replicas, readiness probes, rollouts, and gateway routes.
---

A Service is Axern's long-lived workload: it keeps a target replica count
healthy, rolls configuration changes across replicas, and exposes container
ports through the public `/svc` gateway route. One-shot commands use a
[Run](/guides/run/); event handlers use a [Function](/guides/functions/).

## Create a Service

```bash
axern service create --file service.yaml --wait
```

A Service spec uses the same strict `axern/v1` envelope as a Run and adds
replica and probe semantics:

```yaml title="service.yaml"
api_version: axern/v1
kind: Service
metadata:
  namespace: default
spec:
  source:
    image: docker.io/library/python:3.12-slim
  command:
    argv: [python, -m, http.server, "8080"]
  runtime_class: runc
  replicas: 2
  readiness:
    http:
      port: 8080
      path: /
  resources:
    requests:
      cpu: 500m
      memory: 512Mi
```

`spec.source` selects exactly one of `image`, `template`, or `environment`.
`readiness` and `liveness` accept an `http` probe (`port`, `path`, `scheme`)
or a `tcp_port`, plus `initial_delay`, `period`, `timeout`,
`success_threshold`, and `failure_threshold` durations. `autoscaling` sets
`min_replicas` and `max_replicas`. Use `runc` for trusted long-running
services and `runsc` when the workload handles untrusted input; see
[Runtime and Resources](/architecture/resources/).

## Reach the Service through the gateway

The gateway routes HTTP traffic to a ready replica by port number or port
name:

```text
<context.service_url>/svc/<namespace>/<service-id>/<port>/<path>
```

Service state is authoritative in the control plane; the gateway resolves and
forwards without owning placement.

## Inspect and update

```bash
axern service list --namespace default
axern service get <service-id> --output json
axern service replicas <service-id>
axern service events <service-id>
```

A Service reports one of `reconciling`, `ready`, `degraded`, `failed`,
`deleting`, or `deleted`. `service replicas` views `all`, `current`,
`updated`, `outdated`, `unhealthy`, or `ended` replica allocations.

Updates are explicit rollouts with optimistic concurrency:

```bash
axern service update <service-id> --replicas 3
axern service update <service-id> --environment-id <environment-id> \
  --max-surge 1 --max-unavailable 0
axern service delete <service-id>
```

An update changes replicas, the source environment, execution configuration,
or the rollout policy, and replaces replicas accordingly; replica views show
`updated` versus `outdated` allocations while a rollout drains. Use
`--expected-version` for optimistic concurrency when several actors update the
same Service.

## Tunnel into a replica

For a local upstream that must appear inside the Service network, open a
reverse tunnel to a ready replica:

```bash
axern service tunnel <service-id> --to 127.0.0.1:8080
```

See [Reverse Tunnels](/guides/tunnels/) for session lifecycle, inspection,
and revocation. A complete SDK walkthrough, including gateway verification and
cleanup, is in [Python Service](/guides/python-service/).
