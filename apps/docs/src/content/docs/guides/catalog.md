---
title: Catalog
description: Discover environment templates curated by the platform.
---

The catalog is the control plane's registry of environment templates. A template defines a reproducible sandbox rootfs and its execution policy.

```bash
axern catalog list
axern catalog get python311
```

Templates such as `python311` or `coding-base` give workloads a reproducible, platform-curated rootfs. Every Run and SDK Sandbox selects exactly one source: a catalog template, a generic OCI image, or an existing environment. Prefer generic OCI images for simple experiments; introduce templates where their catalog and reuse semantics matter.

Capturing a template or image as an immutable, reusable Environment — and the namespace and quota rules around it — is covered in [Environments, Namespaces, and Quota](/guides/environments/).

Agent and tool images are caller-owned inputs. Axrun accepts an explicit immutable image reference and passes it through the ordinary read-only `ImageMount` capability.
