---
title: Runtime and Resources
description: Choose between runsc and runc, and size workloads with requests, limits, and namespace quota.
---

Axern runs every workload behind one resource and lifecycle model, with the
runtime class selected per workload. Choose the class by trust, not by
workload duration:

- **`runsc`** is the recommended isolation boundary for untrusted,
  agent-generated code. It adds a user-space kernel between the workload and
  the host.
- **`runc`** is the performance-oriented choice for trusted, long-running
  services that need full host-kernel compatibility.

```bash
axern run --runtime-class runsc docker.io/library/python:3.12-slim -- \
  python -c 'print("hello")'
```

## Requests and limits

Resource intent has three layers: `request` is the scheduler and admission
reservation, `limit` is the runtime hard cap enforced through node-local
cgroups, and namespace `quota` is the admission ceiling. If a request is
omitted, the control plane reserves a default of `500m` CPU and `4GiB` memory.
The invariant is `0 < request <= limit` when a matching limit is set.

```bash
axern run --template python311 \
  --request-cpu 500m \
  --request-memory 512MiB \
  --limit-memory 1GiB \
  -- python -c 'print("hello")'
```

Runs and services use the same resource flags; mutable service resources
change with `axern service update`.

## Namespace quota

Quota caps how much CPU and memory a namespace can reserve across active
workloads. Omitted fields are unlimited; lowering quota below current usage
keeps running workloads alive but blocks new admissions.

```bash
axern namespace create team-a
axern quota set --namespace team-a --cpu 4 --memory 32GiB
axern quota get --namespace team-a
```

Quota and node admission are separate gates: quota answers whether the
namespace may reserve more, node admission answers whether an eligible node
has remaining capacity. Memory is strict for both; only CPU can be
overcommitted, and overcommit changes admission capacity only, never cgroup
limits.

When admission fails, JSON output exposes a stable `diagnostic_code` and a
compact `admission_summary` such as `namespace quota exceeded` or
`node memory capacity exhausted`:

```bash
axern service get <service-id> --output json
```

The repository's
[resource model](https://github.com/cofy-x/axern/blob/main/docs/architecture/resource-model.md)
is the engineering source of truth.
