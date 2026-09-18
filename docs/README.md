# Axern Documentation Guide

Use this directory for durable product, architecture, verification, and operational context. Agent rules belong in `AGENTS.md` and `.x/`; module package maps and commands belong beside the code.

The public documentation website lives in [`apps/docs`](../apps/docs/README.md). It owns user journeys, installation guidance, SDK entry pages, localized content, and the deployed site at `axern.cofy-x.space`. Do not duplicate maintainer runbooks or detailed internal architecture here and in the public site: public pages summarize stable user-facing concepts and link to the authoritative repository document when deeper engineering detail is useful.

## Document Types

| Type         | Purpose                                                 | Maintenance rule                                                                            |
| :----------- | :------------------------------------------------------ | :------------------------------------------------------------------------------------------ |
| Product      | Long-term user model, direction, and product boundaries | Describe intended outcomes and stable concepts; do not use as a task backlog                |
| Architecture | Current system model and cross-component contracts      | Update with the implementation when ownership or behavior changes                           |
| Decision     | A non-obvious choice that constrains future designs     | Create only when the rationale and revisit condition must outlive the implementation change |
| Verification | Reproducible acceptance scope                           | Keep commands executable and distinguish required checks from optional truth checks         |
| Operations   | Repeatable development or deployment runbook            | Prefer commands and observable outcomes; remove superseded steps                            |

Follow the [Markdown Formatting](../.x/coding-standards.md#markdown-formatting) rules rather than maintaining a separate formatting convention here.

## Reading And Authority

This is a lookup index, not a required reading list. Follow [Task-Scoped Reading](../AGENTS.md#task-scoped-reading). The document types above define authority; agent contracts retain short safety constraints and route to those explanations.

When a rule already has an owner, link to it instead of adding another full explanation to an AGENTS file, README, or `.x` page. Keep critical prohibitions visible in agent contracts, but update detailed behavior at its authoritative source. If code and a documented contract disagree, establish the intended behavior and report the discrepancy; do not silently treat either duplication or implementation drift as a new contract.

Do not keep completed plans, migration diaries, dated progress summaries, or alternative designs here after the current contract is established. Git history preserves the process. If a non-obvious decision will constrain future work, record it as a short `docs/decisions/<topic>.md` containing the decision, rationale, consequences, and revisit condition. Create `docs/decisions/` only when the first durable decision is needed. Mark a replaced decision as superseded and link its replacement; keep current behavior in architecture documents.

## Decisions

- [Documentation Site Visual Direction](decisions/docs-site-visual-direction.md): durable visual, content, interaction, and ownership constraints for the public documentation site.
- [Firecracker Evaluation Boundary](decisions/firecracker-evaluation-boundary.md): keep runsc as the sole production backend until a separately qualified experiment proves a concrete need.
- [Node Operator Authority Boundary](decisions/node-operator-authority-boundary.md): retain Allocation-scoped node diagnostics without creating a second lifecycle authority or sharing operator privilege with machine integrations.
- [Verification Feedback Tiers](decisions/verification-feedback-tiers.md): separate fast development feedback, Linux correctness, full regression, and frozen-candidate qualification.

## Product Direction And User Models

- [Long-Term Direction](product/product-direction.md): product north star, durable principles, investment areas, and non-goals.
- [Stable Domain Model](product/domain-model.md): normative product objects, ownership, lifecycle meaning, and boundaries for API and persistence design.
- [SDK User Model](product/sdk-user-model.md): intended SDK concepts and common lifecycle contract.

## Architecture

- [Runtime Architecture](architecture/runtime-architecture.md): concise current control-plane and node-runtime model.
- [Observed Capability Providers](architecture/observed-capability-providers.md): typed node observations, capability policy, transactional admission, and allocation enforcement.
- [Sandbox Network Policy](architecture/sandbox-network-policy.md): strict fail-closed egress, DNS-only deny semantics, canonical rules, and admission requirements.
- [Execution Lifecycle](architecture/execution-lifecycle.md): end-to-end Run and Allocation ownership, transitions, data plane, cleanup, and failure guarantees.
- [Resource Model](architecture/resource-model.md): requests, limits, quota, admission, and diagnostics.
- [Principal And Namespace Authorization](architecture/authorization.md): public mTLS identity mapping, scoped roles, gateway trust, and rotation.
- [Storage Architecture](architecture/storage-architecture.md): durable control state, Allocation-local writable data, image mounts, output transfer, and cleanup.
- [Nydus Image Runtime](architecture/nydus-image-runtime.md): Nydus mount, caching, deduplication, and scaling model.

For a module-internal design, prefer that module's `docs/` directory. Promote material here only when multiple modules need the same model.

## Verification

- [Verification Tiers](verification/local-full-verification.md): change-selected fast checks, Linux correctness, asynchronous full regression, and release qualification boundaries.
- [Dependency License Policy](legal/dependency-licenses.md): release dependency inventory and incompatible-license gate.

## Releases And Operations

- [`releases/`](releases/): version-specific behavior, compatibility, and upgrade history. Release notes are historical records, not current architecture.
- [Devbox Workflow](operations/devbox.md): Linux source-development stack, service restart, and debugging.
- [Release Operations](operations/releases.md): immutable versioning, GHCR and Helm publication, and fresh-cluster acceptance.
- [Runtime Logs](operations/runtime-logs.md): critical logs, node-local paths, and symptom routing.
- [Kubernetes Helm Chart](../deploy/helm/axern/README.md): cloud-neutral chart, scheduling, storage, observability, and networking configuration.
