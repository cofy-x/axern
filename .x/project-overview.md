# Project Overview

Axern is Cofy-X's programmable execution platform for isolated AI-agent,
coding, and general sandbox workloads. The repository contains product APIs and
CLIs, a durable control plane, an external gateway, node-local runtime services,
networking, SDKs, and deployment tooling.

## Repository Map

| Path | Stable role |
| :--- | :--- |
| `apps/` | Executable product entrypoints |
| `control/` | Durable control-plane services |
| `gateway/` | External control and data-plane edge |
| `runtime/` | Node-local execution, image, tunnel, and volume services |
| `network/` | Host networking data planes |
| `sdk/` | Public SDKs and protobuf contracts |
| `lib/` | Internal libraries shared by multiple modules |
| `deploy/` | Local, kind, image, and Helm deployment surfaces |
| `mk/`, `Makefile` | Root orchestration and subsystem wrappers |
| `docs/` | Product direction, current architecture, durable decisions, verification, and runbooks |
| `examples/` | Maintained user-facing examples |

`apps/docs` is the publishable public documentation application. The root
`docs/` tree remains the source of truth for engineering architecture,
maintainer operations, verification contracts, and durable product design.

The [Module Guide](module-guide.md) maps active modules to their local
contracts. Do not maintain a second module inventory here.

## Language Workspaces

Axern keeps four first-class language workspaces. Their configuration files are
the authoritative membership lists:

| Language | Manager | Configuration |
| :--- | :--- | :--- |
| Go | Go workspaces | `go.work` |
| Rust | Cargo | `Cargo.toml` |
| TypeScript | pnpm | `pnpm-workspace.yaml` |
| Python | uv | `pyproject.toml` |

When membership changes, update the configuration, lockfile if applicable,
root orchestration, and module routing in the same change. Do not copy the full
member list into documentation.

## Root Build Model

- The root `Makefile` is the stable entrypoint; bare `make` and `make help`
  expose the supported wrapper surface.
- `mk/root.mk` owns repository-wide bootstrap, build, test, lint, formatting,
  protobuf, and documentation checks.
- `mk/subsystems/*.mk` provides thin namespaced wrappers. Subsystem Makefiles
  remain authoritative for module-specific commands.
- `mk/devbox.mk` owns the Linux source-development environment and standalone
  daemon stack.
- `mk/dev-env.mk` owns local Compose, kind, registry, image, context, and smoke
  workflows.
- `mk/deploy.mk` owns cloud-neutral Helm validation and Kubernetes deployment helpers.

Use `make help` for commands and the owning module README for details. Command
inventories in documentation are explanatory, not authoritative.

## Development Environments

- The repository devbox is the primary source-development environment for the
  Linux node stack. It runs repo-local Postgres and Axern daemons directly and
  keeps generated sockets, logs, state, and configs under `.dev/`.
- Docker Compose and repo-managed kind are local truth environments for
  integration and deployment verification; they are not required for every
  source edit.
- macOS is suitable for host-safe unit, lint, and build checks. FUSE, mount,
  cgroup, namespace, eBPF, runc, and runsc behavior requires the Linux
  validation named by the owning module contract.

See the [Devbox runbook](../docs/operations/devbox.md) and
[Local Deployment](../deploy/local/README.md) for concrete operations.

## Standard Change Flow

1. Find the owner in the [Module Guide](module-guide.md).
2. Read that module's contract when present and its README.
3. Make the smallest cohesive change inside the owning boundary.
4. Run the local contract's required checks.
5. For a cross-module change, verify the boundary described by the
   [Runtime Stack](runtime-stack.md).
6. Update only the durable docs whose contract or workflow changed.
