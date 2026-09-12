# Documentation Site Agent Contract

## Purpose

`apps/docs` is Axern's public, localized Starlight documentation site. Read the [site README](README.md) for commands and the [visual direction decision](../../docs/decisions/docs-site-visual-direction.md) for durable presentation constraints.

## Ownership Boundaries

- English content is normative; Simplified Chinese content is maintained under `zh-cn` with Starlight fallback.
- Present the execution model and capabilities defined by the [Stable Domain Model](../../docs/product/domain-model.md). Summarize repository architecture and operations rather than creating a second source of truth.
- Match examples to the owning CLI, SDK, protobuf, configuration, and deployment contracts. Keep provider account setup, credentials, and regional orchestration outside the public site.
- Keep the site static and use Starlight's navigation, accessibility, localization, and Pagefind capabilities. Keep custom UI small and reproducible assets derived from checked-in sources.
- Pin deployment tooling and GitHub Actions used by the site.

## Validation

- Run `make docs-verify`; it covers content checks, the static build, and built-link validation.
- Run `make docs-assets` for recording changes, `make docs-layout-check` for shared layout changes, and `make docs-social-card` for social-preview source changes.
