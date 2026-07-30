---
title: SDKs
description: Choose the Axern SDK for Python, Go, or TypeScript.
---

All three SDKs expose the same programmable sandbox boundary: source,
lifecycle, execution, attached processes, files, archive transfer, tunnels,
metadata, and typed errors. Mutating RPCs are never retried implicitly.

| SDK | Best fit | Async model | Start |
| --- | --- | --- | --- |
| Python | Agent tooling, notebooks, orchestration | Sync and asyncio | [Python SDK](/sdk/python/) |
| Go | Services and infrastructure controllers | `context.Context` | [Go SDK](/sdk/go/) |
| TypeScript | Node.js applications and tools | Promise APIs | [TypeScript SDK](/sdk/typescript/) |

Install the official packages from their public registries:

- Python: `uv add axern-sdk` from [PyPI](https://pypi.org/project/axern-sdk/)
- Go: `go get github.com/cofy-x/axern/sdk/go@latest` from the
  [Go module index](https://pkg.go.dev/github.com/cofy-x/axern/sdk/go)
- TypeScript: `pnpm add @cofy-x/axern-sdk` from
  [npm](https://www.npmjs.com/package/@cofy-x/axern-sdk)

Each sandbox selects exactly one source. Use an OCI image as the portable
default, a catalog template for a named reusable environment, or an environment
ID to continue working with an existing environment. Lifecycle and data-plane
operations flow through Axern public APIs; SDKs do not add SSH or shell
fallbacks for platform behavior.

Axern publishes the CLI, Helm chart, runtime images, and all three SDKs under
one repository version. Python and TypeScript use public package registries;
Go uses the versioned `sdk/go` module. Treat a pre-1.0 Axern release as one
coherent unit, and commit your package-manager lockfile or resolved `go.mod`
version for repeatable builds.
