# Axern Documentation Guide

Use this directory for durable product, architecture, verification, and
operational context. Agent rules belong in `AGENTS.md` and `.x/`; module package
maps and commands belong beside the code.

The public documentation website lives in [`apps/docs`](../apps/docs/README.md).
It owns user journeys, installation guidance, SDK entry pages, localized
content, and the deployed site at `axern.cofy-x.space`. Do not duplicate
maintainer runbooks or detailed internal architecture here and in the public
site: public pages summarize stable user-facing concepts and link to the
authoritative repository document when deeper engineering detail is useful.

## Document Types

| Type | Purpose | Maintenance rule |
| :--- | :--- | :--- |
| Product | Long-term user model, direction, and product boundaries | Describe intended outcomes and stable concepts; do not use as a task backlog |
| Architecture | Current system model and cross-component contracts | Update with the implementation when ownership or behavior changes |
| Decision | A non-obvious choice that constrains future designs | Create only when the rationale and revisit condition must outlive the implementation change |
| Verification | Reproducible acceptance scope | Keep commands executable and distinguish required checks from optional truth checks |
| Operations | Repeatable development or deployment runbook | Prefer commands and observable outcomes; remove superseded steps |

Do not keep completed plans, migration diaries, dated progress summaries, or
alternative designs here after the current contract is established. Git history
preserves the process. If a non-obvious decision will constrain future work,
record it as a short `docs/decisions/<topic>.md` containing the decision,
rationale, consequences, and revisit condition. Create `docs/decisions/` only
when the first durable decision is needed. Mark a replaced decision as
superseded and link its replacement; keep current behavior in architecture
documents.

## Decisions

- [Documentation Site Visual Direction](decisions/docs-site-visual-direction.md):
  durable visual, content, interaction, and ownership constraints for the
  public documentation site.

## Product Direction And User Models

- [Long-Term Direction](product/product-direction.md): product north star, durable
  principles, investment areas, and non-goals.
- [SDK User Model](product/sdk-user-model.md): intended SDK concepts and common
  lifecycle contract.
- [Function User Model](product/function-user-model.md): Function resource and
  command model.
- [FunctionControl API](product/function-control-proto-design.md): control API
  ownership and RPC surface.
- [Axrun Architecture](../apps/axrun/docs/architecture.md): product-owned agent
  execution and trajectory model.

## Architecture

- [Runtime Architecture](architecture/runtime-architecture.md): concise current
  control-plane and node-runtime model.
- [Workload Lifecycle](architecture/workload-lifecycle-sequence.md): end-to-end
  control and sandbox data-plane sequences.
- [Resource Model](architecture/resource-model.md): requests, limits, quota,
  admission, and diagnostics.
- [Principal And Namespace Authorization](architecture/authorization.md):
  public mTLS identity mapping, scoped roles, gateway trust, and rotation.
- [Storage Architecture](architecture/storage-architecture.md): storage
  control-plane and node-local volume ownership.
- [Nydus Image Runtime](architecture/nydus-image-runtime.md): Nydus mount,
  caching, deduplication, and scaling model.

For a module-internal design, prefer that module's `docs/` directory. Promote
material here only when multiple modules need the same model.

## Verification

- [Local Full Verification](verification/local-full-verification.md): concise
  repository, Compose, kind, and Nydus verification checklist.
- [Dependency License Policy](legal/dependency-licenses.md): release dependency
  inventory and incompatible-license gate.

## Development And Operations

- [v0.3.0 Release Notes](releases/v0.3.0.md): authorization, diagnostics, and
  the clean-install boundary for the v0.3 platform baseline.
- [Devbox Workflow](operations/devbox.md): Linux source-development stack,
  service restart, and debugging.
- [Release Operations](operations/releases.md): immutable versioning, GHCR and
  Helm publication, and fresh-cluster acceptance.
- [Runtime Logs](operations/runtime-logs.md): critical logs, node-local paths,
  and symptom routing.
- [Startup And Readiness Contract](operations/startup-readiness-performance-contract.md):
  performance metrics, stages, cache states, and acceptance gates.
- [Kubernetes Helm Chart](../deploy/helm/axern/README.md): cloud-neutral chart,
  scheduling, storage, observability, and networking configuration.
- [Agent Runtime](../apps/cli/docs/agent.md): run coding agents in an Axern
  allocation while credentials remain local.
