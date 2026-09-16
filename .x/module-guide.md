# Module Guide

Use this page to identify the owner of a task and then switch to that module's local context. Workspace files, not this page, are authoritative for build membership.

## Product And API Surfaces

| Module           | Responsibility                                                                            | Local context                                                                  |
| :--------------- | :---------------------------------------------------------------------------------------- | :----------------------------------------------------------------------------- |
| `apps/axrun`     | Agent harness, task execution, verification, and trajectory capture                       | [Contract](../apps/axrun/AGENTS.md), [README](../apps/axrun/README.md)         |
| `apps/cli`       | Product CLI for Axern control and data-plane workflows                                    | [Contract](../apps/cli/AGENTS.md), [README](../apps/cli/README.md)             |
| `apps/docs`      | Public documentation website, localized user guides, and publishable documentation assets | [Contract](../apps/docs/AGENTS.md), [README](../apps/docs/README.md)           |
| `sdk/proto`      | Shared public and internal protobuf contracts and generation entrypoints                  | [README](../sdk/proto/README.md)                                               |
| `sdk/go`         | Go SDK                                                                                    | [README](../sdk/go/README.md)                                                  |
| `sdk/python`     | Python SDK                                                                                | [Contract](../sdk/python/AGENTS.md), [README](../sdk/python/README.md)         |
| `sdk/typescript` | TypeScript SDK                                                                            | [Contract](../sdk/typescript/AGENTS.md), [README](../sdk/typescript/README.md) |

## Control, Gateway, And Node Stack

| Module | Responsibility | Local context |
| :-- | :-- | :-- |
| `control/controld` | Durable product semantics, placement, allocation lifecycle, routing, and control state | [Contract](../control/controld/AGENTS.md), [README](../control/controld/README.md) |
| `gateway/gatewayd` | Unified external control edge plus Allocation-scoped process, file, archive, terminal, SSH, and Tunnel forwarding | [Contract](../gateway/gatewayd/AGENTS.md), [README](../gateway/gatewayd/README.md) |
| `runtime/axnoded` | Node-local sandbox lifecycle, execution, and allocation cleanup | [Contract](../runtime/axnoded/AGENTS.md), [README](../runtime/axnoded/README.md) |
| `runtime/egressd` | Trusted node-local sandbox egress policy lifecycle and enforcement | [Contract](../runtime/egressd/AGENTS.md), [README](../runtime/egressd/README.md) |
| `runtime/tunneld` | Internal reverse-TCP relay and node-local tunnel binding | [Contract](../runtime/tunneld/AGENTS.md), [README](../runtime/tunneld/README.md) |
| `runtime/imagemgr` | Image rootfs resolution and OCI and Nydus mount orchestration | [Contract](../runtime/imagemgr/AGENTS.md), [README](../runtime/imagemgr/README.md) |
| `runtime/imagefsd` | Read-only FUSE image data plane and chunk serving | [Contract](../runtime/imagefsd/AGENTS.md), [README](../runtime/imagefsd/README.md) |
| `network/bpfnet` | Optional eBPF host networking data plane | [Contract](../network/bpfnet/AGENTS.md), [README](../network/bpfnet/README.md) |

## Shared And Repository Infrastructure

| Area | Responsibility | Context |
| :-- | :-- | :-- |
| `lib/go` | Internal Go libraries shared by Axern services and SDKs | [README](../lib/go/README.md) |
| `deploy/` | Compose, kind, image, and cloud-neutral Helm deployment surfaces | [Local deployment](../deploy/local/README.md), [Helm chart](../deploy/helm/axern/README.md) |
| `mk/`, `Makefile` | Root orchestration and thin subsystem wrappers | [Project Overview](project-overview.md), `make help` |
| `docker/`, `scripts/devbox/`, `.dev/` | Repository Linux development environment and generated local state | [Devbox runbook](../docs/operations/devbox.md) |

If a task spans rows, read each affected local contract and use the [Runtime Stack](runtime-stack.md) to select the relevant cross-component contract. README links are routing and command references, not a requirement to read every module overview. Add a local `AGENTS.md` only for distinct constraints or task routes, not to repeat the root contract or copy architecture prose. A deeper contract belongs in its nearest indexed module and must be routed from that module's `AGENTS.md`; it does not belong in this root guide.
