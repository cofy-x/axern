# Axern Product Direction

This document defines the durable direction used to accept or reject scope. The [single-host architecture decision](../decisions/single-host-agent-serving.md) defines the target and module dispositions; the [roadmap](https://github.com/cofy-x/axern/issues/195) tracks implementation and acceptance. Current API and lifecycle guarantees remain in the [Domain Model](domain-model.md) and architecture documents.

## North Star

Axern is single-host execution and persistent workspace infrastructure for agents. Its implemented foundation is gVisor-isolated execution; persistent Workspaces are the next data capability, not an available API. Personal agent systems on one Linux host are the primary design reference, with reconnectable work and low operational overhead. Evaluation, training, and data synthesis remain consumers of the same execution primitives.

The target experience is to run independent agent workers, retain their work, inspect processes and services, and hand work between agents and a human without operating a cluster. Success is measured by time to a first useful task, recovery after interruption, idle resource cost, and ongoing maintenance effort. Unmeasured startup, density, or durability claims are not product guarantees.

## Delivery Status

The current product provides the `Environment -> Run -> Allocation` execution model, a Sandbox SDK facade, process/file/terminal operations, reverse Tunnels, read-only image mounts, bounded declared outputs, and successful-Run rootfs sealing into an immutable Environment. Writable files remain Allocation-local; neither output retention nor rootfs sealing provides a mutable persistent Workspace.

An experimental native Linux profile now provides a source-built archive, systemd installation and lifecycle, required resource/network enforcement, and OCI-only image management. It has been built and exercised on fresh installations; [#197](https://github.com/cofy-x/axern/issues/197) owns the platform-specific acceptance evidence and remaining qualification work. This is separate from the Compose development path, which uses development credentials and `disabled_dev` cgroup enforcement. Native installation is not yet a published release channel or a supported upgrade/backup contract.

Persistent Workspace, retained-result handoff, and the complete stateful maintenance contract remain planned. This direction does not introduce those APIs, change output retention, or promise process survival across reboot. The [consumer experiment](https://github.com/cofy-x/axern/issues/213) uses existing public APIs to expose integration gaps; its plan is not evidence of Workspace or full agent-pilot acceptance.

## Principles

- **Single-host first:** make one Linux installation, configuration, data layout, and maintenance path complete before investing in cluster expansion. macOS remains a client and a development host through a Linux VM; it is not a native runsc execution host.
- **One runtime:** gVisor/runsc is the sole long-term sandbox backend. Missing isolation or capability evidence fails closed; no fallback or second runtime registry is planned.
- **One execution model:** `Environment -> Run -> Allocation -> runsc sandbox` remains the only durable execution chain. Sandbox is the SDK facade; a persistent Workspace will own retained data separately from execution.
- **Explicit authority:** process, file, archive, terminal, SSH, Tunnel, and future preview access remain scoped, revocable, and bound to execution identity. Packaging consolidation must preserve public, operator, and privileged runtime boundaries.
- **Durable control, local data:** PostgreSQL remains the sole authoritative product-state backend in this transition. Node-local recovery is a projection. Host-local files need an explicit retention, capacity, backup, and loss contract; metadata durability alone does not make bytes durable.
- **Recoverable work:** distinguish reconnecting to an existing execution, rebuilding a new execution from retained data, and losing a process. The caller owns semantic retries and duplicate external-effect prevention.
- **Small operational surface:** manage dependencies and compatible control components together, retain necessary privilege separation, and make optional image/network optimizations earn their cost through real workloads.
- **Public composition:** worker adapters, CLIs, and SDK consumers use public APIs. No particular model provider, agent harness, Kubernetes installation, or image-build service is required to consume the platform.

## Four Concepts, One Execution Chain

These concepts separate immutable inputs, retained data, execution, and caller intent. They are not four new public resource roots. The durable execution chain remains `Environment -> Run -> Allocation -> runsc sandbox`.

| Concept | Ownership and lifetime | Current boundary |
| --- | --- | --- |
| Image | Immutable runtime, tools, and reference content, addressed by digest | An Environment resolves execution input and policy; read-only ImageMounts add image content. Image building and registry retention remain external responsibilities. |
| Workspace | Planned namespace-scoped retained working data, independent of any one execution | No public Workspace attachment exists today. The first version targets one enforced writer, explicit capacity, retention, and deletion. |
| Sandbox | One execution's runtime view, including its filesystem and processes | The SDK facade operates an explicit Run/Allocation. Replacing it creates new execution identities and does not reuse old access handles. |
| AgentSession | Caller-owned conversation, configuration, model policy, planning, and coordination | Its files may be execution inputs or retained data, but Axern does not interpret session semantics or introduce an Agent/Session control-plane resource. |

The default multi-agent layout gives each worker an independent writable working directory, shares immutable reference inputs read-only, and publishes selected results for explicit caller-owned merging or handoff. Current public APIs support independent Allocation filesystems, read-only image mounts, and export/restore through file or output APIs. They do not support arbitrary host binds or a shared writable volume. Caller-managed export/restore must be described as such, not as native Workspace persistence.

The first Workspace version retains this independent-writer layout. Simultaneous multi-writer access to shared writable data is an unresolved requirement outside that initial contract; it needs an explicit consistency, authorization, accounting, fencing, and recovery decision before implementation. Sharing published artifacts does not imply sharing a live writable filesystem.

## Product Boundary

| Area | Axern scope | Caller or external scope |
| --- | --- | --- |
| Execution | Environment, Run, Allocation, admission, limits, cancellation, cleanup, and observable recovery | Task intent, planning, Agent roles, orchestration, and semantic retries |
| Work data | Planned host-local Workspace identity, authorized attachment, single-writer fencing, retention, and explicit deletion | Agent memory and session meaning, worker configuration, result/Git merging, and distributed storage |
| Interaction | Processes, terminals, files, logs, reverse Tunnels, and planned authenticated allocation-scoped previews | IDE or dashboard product, application rollout, replicas, and route orchestration |
| Delivery | Existing bounded outputs and planned explicit retained-result handoff | Business acceptance, long-term external publication, and external side effects |
| Authority | Principal/Namespace authorization, secrets, execution authority, and resource accounting | Human approval policy, model budgets, provider credentials, and event integrations |
| Operations | Experimental native installation/lifecycle and diagnostics; planned supported-version upgrades and coordinated backup/restore | Cloud accounts, cluster provisioning, high availability, and image builds |

The planned Workspace is deliberately narrow: managed local data with an independent lifetime, not a general volume platform. Stopping an execution must not imply deleting its retained Workspace; competing writer requests require enforced ownership rather than a shared-directory convention. The exact public contract must be implemented atomically across state, APIs, SDKs, CLI, and verification before the Domain Model presents it as available.

Existing Kubernetes, Nydus, and other supported paths are not removed by this direction. New cluster/HA, distributed image/storage optimization, and transparent process snapshot/fork investment is deferred behind demonstrated single-host needs. Additional execution runtimes are outside the chosen product scope.

## Scope Test

A proposal belongs in Axern when it serves isolated execution or retained execution data, has one explicit state owner, preserves the execution chain, and defines authorization, capacity, cleanup, and recovery. Workflow meaning and provider-specific behavior stay in callers. A new first-class resource must demonstrate an independent identity and lifetime that existing resources cannot safely express.
