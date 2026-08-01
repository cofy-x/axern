---
title: Catalog and Environments
description: Discover runtime templates and agent bundles, and capture them as immutable environments.
---

The catalog is the control plane's registry of runtime templates and agent
bundles. A template defines a sandbox rootfs; a bundle defines a read-only
agent or tool image mounted into a sandbox.

```bash
axern catalog list
axern catalog get python311
axern catalog bundle list
axern catalog bundle get <bundle-id>
```

Templates such as `python311` or `coding-base` give workloads a reproducible,
platform-curated rootfs. Every Run, Service, Function, and SDK sandbox selects
exactly one source: a catalog template, a generic OCI image, or an existing
environment. Prefer generic OCI images for simple experiments; introduce
templates where their catalog and reuse semantics matter.

## Environments

An environment captures a source as an immutable, reusable platform resource.
Create one from a template or an OCI image reference:

```bash
axern environment create --template-id python311 --label team=runtime
axern environment create --image-ref docker.io/library/python:3.12-slim
axern environment list
```

Private registries use a stored credential:

```bash
axern environment create \
  --image-ref registry.example.com/team/base:1.0 \
  --registry-credential-id <secret-id>
```

Environments never mutate after creation. SDKs accept an `environment_id`
source so repeated sandboxes share one reviewed, platform-managed base. Agent
bundles are normally resolved implicitly: `axern agent` mounts the matching
`codex` or `claude-code` bundle, and Axrun freezes the bundle digest at
rollout planning time.
