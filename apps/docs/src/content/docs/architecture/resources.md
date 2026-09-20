---
title: Runtime and Resources
description: Run isolated workloads with runsc and size them with requests, limits, and namespace quota.
---

Axern uses gVisor (`runsc`) as its production sandbox runtime. It adds a user-space kernel between the workload and the host, under one resource and lifecycle model. Runsc is the supported execution runtime, with no automatic runtime fallback.

```bash
axern run docker.io/library/python:3.12-slim -- \
  python -c 'print("hello")'
```

## Requests and limits

Resource intent has three layers: `request` becomes the immutable Allocation charge used by placement and admission, `limit` is the runtime hard cap enforced through node-local cgroups, and namespace `quota` is the admission ceiling. If a request is omitted, the control plane applies a default of `500m` CPU and `4GiB` memory. The invariant is `0 < request <= limit` when a matching limit is set. A bound Allocation keeps its charge through `RELEASING`; capacity returns only after cleanup reaches `RELEASED`.

```bash
axern run python:3.12-slim \
  --request-cpu 500m \
  --request-memory 512MiB \
  --limit-memory 1GiB \
  -- python -c 'print("hello")'
```

Every Run declares its resource intent at creation time.

## Namespace quota

Quota caps the CPU and memory charged by non-released Allocations in a namespace. Omitted fields are unlimited; lowering quota below current usage keeps running workloads alive but blocks new admissions.

```bash
axern namespace create team-a
axern quota set --namespace team-a --cpu 4 --memory 32GiB
axern quota get --namespace team-a
```

Quota and node admission are separate gates: quota answers whether the namespace may accept another Allocation charge, while node admission answers whether an eligible node has remaining capacity. Memory is strict for both; only CPU can be overcommitted, and overcommit changes admission capacity only, never cgroup limits.

When admission fails, JSON output exposes a stable `diagnostic_code`. The accompanying `message` is human-readable context and must not be parsed as a machine contract:

```bash
axern run get <run-id> --output json
```

The repository's [resource model](https://github.com/cofy-x/axern/blob/main/docs/architecture/resource-model.md) is the engineering source of truth.
