# Axern Product Direction

Axern is single-host execution and persistent workspace infrastructure for agents. This document owns product scope; the [Domain Model](domain-model.md) and architecture documents own implemented API and lifecycle contracts. The [single-host decision](../decisions/single-host-agent-serving.md) explains the deployment and ownership choices.

## Outcome

Make it practical to run independent agent workers on one Linux host, inspect their work, retain useful files, and hand work between agents and a human. Personal agent systems are the primary design reference; evaluation, training and data synthesis consume the same execution primitives. Measure progress through time to a useful task, recovery after interruption, idle resource cost and maintenance effort.

## Current Capabilities

The execution model is `Environment -> Run -> Allocation -> runsc sandbox`. SDKs expose Sandbox as a facade over that chain. Current capabilities include processes, files, terminals, SSH, reverse Tunnels, read-only image mounts, bounded declared outputs and successful-Run rootfs sealing into an immutable Environment. Writable files remain Allocation-local; caller export/restore is not a native persistent Workspace.

The [native Linux installation](../../deploy/standalone/INSTALL.md) provides an experimental source-built archive and systemd lifecycle with required resource/network enforcement and OCI image management. [Compose](../../deploy/local/README.md) is the separate development profile. Persistent Workspace, authenticated previews, retained-result handoff and supported stateful upgrades/backup remain planned; their design does not make those APIs available.

## Principles

- **Single-host first:** complete the Linux installation, data layout and maintenance path. macOS is a client and a development host through a Linux VM.
- **One runtime and execution chain:** gVisor/runsc is the sole sandbox backend. Missing required isolation or enforcement fails closed; execution identities are never reused and access remains Allocation-scoped.
- **Explicit authority:** public, operator and privileged runtime responsibilities remain distinct when components share an installation or process.
- **Durable control, local data:** PostgreSQL is the sole authoritative product-state backend. Node recovery files are projections; retained bytes need their own capacity, authorization, deletion, backup and loss contract.
- **Recoverable work:** reconnecting to an existing execution, starting a new execution from retained data and restoring a backup have different authority and failure semantics. Callers own semantic retries and duplicate external-effect prevention.
- **Small operational surface:** manage dependencies together and retain optional optimizations only when actual workloads justify their cost. No particular model provider, Agent harness, cluster or image-build service is required.

## Images, Workspaces, Execution And Sessions

| Concept | Owner and lifetime |
| --- | --- |
| Image | Immutable runtime, tools and reference inputs addressed by digest. Environments resolve those inputs and policy; read-only ImageMounts add content. Axern maintains [workload image recipes](../../images/agents/README.md), shared toolchain versions and local build/check commands. Registry operation and retention remain operator responsibilities. |
| Workspace | Planned namespace-scoped retained data, independent of execution. The first contract has one enforced writer, retained capacity, explicit authorization and deletion; no public attachment API exists yet. |
| Sandbox | SDK/runtime view of one Run and Allocation, including its filesystem and processes. A replacement execution receives new identities. |
| AgentSession | Caller-owned conversation, configuration, model policy, planning and coordination. Axern can execute its worker and carry files without interpreting session meaning. |

Multiple workers use independent writable directories, immutable common references and explicit result publication for caller-owned merging or handoff. Native Workspace design is defined by the [single-writer decision](../decisions/single-writer-workspaces.md). Simultaneous shared writes require a separate consistency and authority contract; sharing results does not grant a second writer.

## Product Boundary

| Area | Axern responsibility | Caller or operator responsibility |
| --- | --- | --- |
| Execution | Admission, limits, cancellation, cleanup and observable recovery through Environment, Run and Allocation | Tasks, planning, Agent roles, orchestration and semantic retries |
| Work data | Allocation-local files and outputs; planned managed host-local Workspace | Agent memory and session meaning, result/Git merging, distributed storage and long-term external publication |
| Interaction | Process, terminal, file, log, SSH and reverse Tunnel access; planned authenticated previews | IDE/dashboard products, application rollout, replicas and route orchestration |
| Authority | Principal/Namespace authorization, Secret projection, execution authority and resource accounting | Human approval policy, model budgets, provider accounts and event integrations |
| Images | Shared toolchains, official Agent installation layers, version/source locks and local build/check commands | Project-specific customization, build-service orchestration, registry credentials and publication |
| Operations | Native installation/lifecycle and diagnostics; planned supported-version upgrades and coordinated backup/restore | Cloud accounts, cluster provisioning, high availability and regional deployment |

Kubernetes and Nydus are optional implemented paths outside the native serving default. Cluster expansion, distributed image/storage optimization and transparent process snapshots require demonstrated single-host needs. Additional execution runtimes are outside the product scope.

A feature belongs in Axern when it serves isolated execution or retained execution data, has one explicit state owner, and defines authorization, capacity, cleanup and recovery. A new public resource must have an independent identity and lifetime that existing resources cannot safely express. Workflow meaning and provider-specific behavior remain in callers.
