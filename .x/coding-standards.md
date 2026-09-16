# Coding Standards

Rules and conventions for Axern contributors and agents.

Product boundaries and general design rules belong to the [Agent Contract](../AGENTS.md). This document adds implementation conventions; use existing subsystem patterns before introducing a new style or helper layer.

## Go Interface Rules

- Define Go interfaces at the consumer boundary. Prefer the smallest capability interface that the caller actually needs.
- Do not pass broad "god interfaces" through application wiring when smaller role-specific interfaces will do.
- Do not rely on type assertions to discover required behavior on the main path. If a capability is required for normal control flow, model it as an explicit interface dependency.
- Do not downcast from an interface back to a concrete implementation in order to continue normal application flow. If implementation-specific behavior is genuinely required, introduce a narrow interface for that behavior instead.
- Keep composition roots and API layers dependent on capabilities, while concrete stores and services implement those capabilities behind the boundary.

## Placement By Language

| Language | Canonical location | Notes |
| :-- | :-- | :-- |
| Go | active `go.work` members | Put product CLI code in `apps/cli`, Go SDK code in `sdk/go`, internal shared libraries in `lib/go`, and platform services in their owning `control/`, `gateway/`, `runtime/`, or `network/` subtree. |
| Rust | `runtime/imagefsd` | Add new Rust workspace members only with a concrete platform owner and root docs updates. |
| TypeScript | `sdk/typescript` | Use ESM only. |
| Python | `sdk/python/src` | Keep importable modules under `src/`. |

## Validation Baseline

Use the [Verification Tiers](../docs/verification/local-full-verification.md) for the repository gate and delivery-stage policy, then the owning module's validation section for focused checks. Prefer existing Make targets and the change planner over maintaining another language or integration test matrix here. Protobuf generation ordering is a hard constraint in the [Agent Contract](../AGENTS.md); generation commands belong to the [Proto Guide](../sdk/proto/README.md).

## Markdown Formatting

- Keep each natural-language paragraph on one source line in both English and Chinese Markdown. Let editors and renderers soft-wrap it for display; do not insert line breaks merely to fit a terminal column width.
- Apply the same rule to prose inside list items and block quotes. Use a blank line for a real paragraph or semantic boundary; a single newline inside prose must not stand in for punctuation or paragraph structure.
- Preserve deliberate line breaks only when source line boundaries carry meaning, including nested lists, tables, code fences, command examples, frontmatter, diagrams, explicit Markdown or HTML breaks, and long URLs that cannot remain readable otherwise.
- Run `make agent-doc-check` after changing repository Markdown; it checks both English and localized prose across `.md` and `.mdx` files.

## Repository Hygiene

- Add a top-level product app or dashboard only when it has an explicit product owner and requirement.
- Keep code, comments, engineering documentation, and normative repository contracts in English. Localized public documentation may use its target language; the same Markdown formatting rules apply.
- Update workspace membership and orchestration together according to the [Project Overview](project-overview.md) when changing repository structure.
