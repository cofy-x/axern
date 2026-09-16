# Project Overview

Use this document for repository layout, workspace membership, and build orchestration changes. Product semantics belong to the [Stable Domain Model](../docs/product/domain-model.md), not this repository map.

## Repository Map

| Path              | Stable role                                                                            |
| :---------------- | :------------------------------------------------------------------------------------- |
| `apps/`           | Executable product entrypoints                                                         |
| `control/`        | Durable control-plane services                                                         |
| `gateway/`        | External control and data-plane edge                                                   |
| `runtime/`        | Node-local execution, image, egress, and tunnel components                             |
| `network/`        | Host networking data planes                                                            |
| `sdk/`            | Public SDKs and protobuf contracts                                                     |
| `lib/`            | Internal libraries shared by multiple modules                                          |
| `deploy/`         | Local, kind, image, and Helm deployment surfaces                                       |
| `mk/`, `Makefile` | Root orchestration and subsystem wrappers                                              |
| `docs/`           | Product direction, current architecture, durable decisions, verification, and runbooks |
| `examples/`       | Maintained user-facing examples                                                        |

`apps/docs` is the publishable public documentation application. The root `docs/` tree remains the source of truth for engineering architecture, maintainer operations, verification contracts, and durable product design.

The [Module Guide](module-guide.md) maps active modules to their local contracts. Do not maintain a second module inventory here.

## Language Workspaces

Axern keeps four first-class language workspaces. Their configuration files are the authoritative membership lists:

| Language   | Manager       | Configuration         |
| :--------- | :------------ | :-------------------- |
| Go         | Go workspaces | `go.work`             |
| Rust       | Cargo         | `Cargo.toml`          |
| TypeScript | pnpm          | `pnpm-workspace.yaml` |
| Python     | uv            | `pyproject.toml`      |

When membership changes, update the configuration, lockfile if applicable, root orchestration, and module routing in the same change. Do not copy the full member list into documentation.

## Root Build Model

- The root `Makefile` is the stable entrypoint; bare `make` and `make help` expose the supported wrapper surface.
- `mk/root.mk` owns repository-wide bootstrap, build, test, lint, formatting, protobuf, and documentation checks.
- `mk/subsystems/*.mk` provides thin namespaced wrappers. Subsystem Makefiles remain authoritative for module-specific commands.
- `mk/devbox.mk` owns the Linux source-development environment and standalone daemon stack.
- `mk/dev-env.mk` owns local Compose, kind, registry, image, context, and smoke workflows.
- `mk/deploy.mk` owns cloud-neutral Helm validation and Kubernetes deployment helpers.

Use `make help` for commands and the owning module README for details. Command inventories in documentation are explanatory, not authoritative.

## Development Environments

- The repository devbox is the primary source-development environment for the Linux node stack. It runs repo-local Postgres and Axern daemons directly and keeps generated sockets, logs, state, and configs under `.dev/`.
- Docker Compose and repo-managed kind are local truth environments for integration and deployment verification; they are not required for every source edit.
- macOS is suitable for host-safe unit, lint, and build checks. FUSE, mount, cgroup, namespace, eBPF, runsc behavior requires the Linux validation named by the owning module contract.

See the [Devbox runbook](../docs/operations/devbox.md) and [Local Deployment](../deploy/local/README.md) for concrete operations.

Task reading and validation follow the [Agent Contract](../AGENTS.md); this overview adds no separate workflow.
