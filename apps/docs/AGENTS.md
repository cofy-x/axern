# Documentation Site Agent Contract

## Purpose

`apps/docs` is the public localized documentation site. Use the [site README](README.md) for commands and the [visual direction](../../docs/decisions/docs-site-visual-direction.md) for presentation constraints.

## Ownership Boundaries

- English is normative; Simplified Chinese lives under `zh-cn` with explicit fallback.
- Present the execution model and capabilities defined by the [Stable Domain Model](../../docs/product/domain-model.md). Summarize repository architecture and operations rather than creating a second source of truth.
- Match examples to the owning CLI, SDK, protobuf, configuration, and deployment contracts. Keep provider account setup, credentials, and regional orchestration outside the public site.
- Keep the site static, accessible, and reproducible; custom UI and assets must not create another product or architecture model.

## Validation

Run `make docs-verify` and the asset, layout, or social-card target selected by the changed surface.
