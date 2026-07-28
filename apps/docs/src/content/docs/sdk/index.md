---
title: SDKs
description: Choose the Axern SDK for Python, Go, or Node.js.
---

All three SDKs expose the same programmable sandbox boundary: source,
lifecycle, execution, attached processes, files, archive transfer, tunnels,
metadata, and typed errors. Mutating RPCs are never retried implicitly.

| SDK | Best fit | Async model | Start |
| --- | --- | --- | --- |
| Python | Agent tooling, notebooks, orchestration | Sync and asyncio | [Python SDK](/sdk/python/) |
| Go | Services and infrastructure controllers | `context.Context` | [Go SDK](/sdk/go/) |
| TypeScript | Node.js applications and tools | Promise APIs | [TypeScript SDK](/sdk/typescript/) |

Each sandbox selects exactly one source: a template, image, or existing
environment. Lifecycle and data-plane operations flow through Axern public
APIs; SDKs do not add SSH or shell fallbacks for platform behavior.

The current source release contains all SDKs in the monorepo. Package registry
publication is a separate release capability; use the repository examples
while evaluating the pre-1.0 API.
