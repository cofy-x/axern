# Axern Product Direction

This document defines the durable direction used to accept or reject scope. The [single-host architecture decision](../decisions/single-host-agent-serving.md) defines the target and module dispositions; the [roadmap](https://github.com/cofy-x/axern/issues/195) tracks implementation and acceptance. Current API and lifecycle guarantees remain in the [Domain Model](domain-model.md) and architecture documents.

## North Star

Axern is a self-hosted execution platform for AI agents, optimized for continued work on one Linux host. It provides isolated execution through gVisor, with a direction toward persistent workspaces, reconnectable work, and low operational overhead. Personal agent systems are the primary design reference; evaluation, training, and data synthesis remain consumers of the same execution primitives.

The target experience is to run independent agent workers, retain their work, inspect processes and services, and hand work between agents and a human without operating a cluster. Success is measured by time to a first useful task, recovery after interruption, idle resource cost, and ongoing maintenance effort. Unmeasured startup, density, or durability claims are not product guarantees.

## Delivery Status

The current product provides the `Environment -> Run -> Allocation` execution model, a Sandbox SDK facade, process/file/terminal operations, reverse Tunnels, and a multi-service local Compose stack. Writable files are Allocation-local; selected outputs have bounded node-local retention. The current local stack uses development credentials and `disabled_dev` cgroup enforcement.

A qualified long-running Linux standalone profile and persistent Workspace are planned capabilities. Updating this direction does not introduce a Workspace API, change output retention, promise process survival across reboot, or turn the local development profile into a production service. Follow the roadmap for implementation evidence.

## Principles

- **Single-host first:** make one Linux installation, configuration, data layout, and maintenance path complete before investing in cluster expansion. macOS remains a client and a development host through a Linux VM; it is not a native runsc execution host.
- **One runtime:** gVisor/runsc is the sole long-term sandbox backend. Missing isolation or capability evidence fails closed; no fallback or second runtime registry is planned.
- **One execution model:** `Environment -> Run -> Allocation -> runsc sandbox` remains the only durable execution chain. Sandbox is the SDK facade; a persistent Workspace will own retained data separately from execution.
- **Explicit authority:** process, file, archive, terminal, SSH, Tunnel, and future preview access remain scoped, revocable, and bound to execution identity. Packaging consolidation must preserve public, operator, and privileged runtime boundaries.
- **Durable control, local data:** PostgreSQL remains the sole authoritative product-state backend in this transition. Node-local recovery is a projection. Host-local files need an explicit retention, capacity, backup, and loss contract; metadata durability alone does not make bytes durable.
- **Recoverable work:** distinguish reconnecting to an existing execution, rebuilding a new execution from retained data, and losing a process. The caller owns semantic retries and duplicate external-effect prevention.
- **Small operational surface:** manage dependencies and compatible control components together, retain necessary privilege separation, and make optional image/network optimizations earn their cost through real workloads.
- **Public composition:** worker adapters, CLIs, and SDK consumers use public APIs. No particular model provider, agent harness, Kubernetes installation, or image-build service is required to consume the platform.

## Product Boundary

| Area | Axern scope | Caller or external scope |
| --- | --- | --- |
| Execution | Environment, Run, Allocation, admission, limits, cancellation, cleanup, and observable recovery | Task intent, planning, Agent roles, orchestration, and semantic retries |
| Work data | Planned host-local Workspace identity, authorized attachment, single-writer fencing, retention, and explicit deletion | Agent memory, conversation/session meaning, Git merging, and distributed storage |
| Interaction | Processes, terminals, files, logs, reverse Tunnels, and planned authenticated allocation-scoped previews | IDE or dashboard product, application rollout, replicas, and route orchestration |
| Delivery | Existing bounded outputs and planned explicit retained-result handoff | Business acceptance, long-term external publication, and external side effects |
| Authority | Principal/Namespace authorization, secrets, execution authority, and resource accounting | Human approval policy, model budgets, provider credentials, and event integrations |
| Operations | A qualified standalone lifecycle, diagnostics, backup/restore, and supported-version upgrades | Cloud accounts, cluster provisioning, high availability, and image builds |

The planned Workspace is deliberately narrow: managed local data with an independent lifetime, not a general volume platform. Stopping an execution must not imply deleting its retained Workspace; concurrent writers require enforced ownership rather than a shared-directory convention. The exact public contract must be implemented atomically across state, APIs, SDKs, CLI, and verification before the Domain Model presents it as available.

Existing Kubernetes, Nydus, and other supported paths are not removed by this direction. New cluster/HA, distributed image/storage optimization, and transparent process snapshot/fork investment is deferred behind demonstrated single-host needs. Additional execution runtimes are outside the chosen product scope.

## Scope Test

A proposal belongs in Axern when it serves isolated execution or retained execution data, has one explicit state owner, preserves the execution chain, and defines authorization, capacity, cleanup, and recovery. Workflow meaning and provider-specific behavior stay in callers. A new first-class resource must demonstrate an independent identity and lifetime that existing resources cannot safely express.
