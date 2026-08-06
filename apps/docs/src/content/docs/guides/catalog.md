---
title: Catalog
description: Discover runtime templates and agent bundles curated by the platform.
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

Capturing a template or image as an immutable, reusable Environment — and the
namespace and quota rules around it — is covered in
[Environments, Namespaces, and Quota](/guides/environments/).

Agent bundles are normally resolved implicitly: `axern agent` mounts the
matching `codex` or `claude-code` bundle, and Axrun freezes the bundle digest
at rollout planning time.
