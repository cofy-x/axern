# Documentation Site Agent Contract

## Purpose

`apps/docs` is Axern's public documentation product. It owns the static
Starlight site, localized user journeys, generated terminal recordings, and
Cloudflare Pages build contract.

Read this file, `apps/docs/README.md`, and
`docs/decisions/docs-site-visual-direction.md` before changing the site.

## Ownership Boundary

- English content is normative. Simplified Chinese content is maintained
  progressively through the `zh-cn` locale and falls back to English with
  Starlight's untranslated-content notice.
- This app owns public installation, CLI, SDK, Axrun, and conceptual user
  documentation.
- Root `docs/` owns detailed engineering architecture, maintainer operations,
  verification contracts, and durable design documents. Summarize those
  contracts here; do not copy them into a second source of truth.
- Runtime API, protobuf, configuration, and deployment semantics remain owned
  by their product modules. Documentation examples must match those sources.
- Cloud-provider account setup, credentials, and regional deployment
  orchestration do not belong in this public app.

## Implementation

- Keep the site fully static. Do not add a server adapter, database, analytics
  backend, or runtime dependency.
- Use Starlight's built-in Pagefind search and locale fallback.
- Keep custom UI small, accessible, responsive, and aligned with Starlight
  rather than replacing its documentation shell.
- Pin Wrangler and every GitHub Action used by the deployment workflow.
- Keep terminal recordings reproducible from the checked-in VHS tapes.

## Validation

Run from the repository root:

```bash
make docs-check
make docs-build
make docs-verify
make agent-doc-check
```

Run `make docs-assets` when a tape or recorded CLI surface changes.
Run `make docs-layout-check` when homepage layout, sidebar structure, or
shared page chrome changes; it requires a local Chrome or Chromium binary
(`CHROME_BIN` overrides discovery) and is not part of `docs-verify`.
Run `make docs-social-card` when the social preview SVG source changes; the
asset check rejects a stale or modified generated PNG.
Run `make docs-service-asset` when the homepage Service recording or its Python
example changes; this target requires a ready local Compose data plane and
must inspect the live Service through the public CLI and leave no Service or
environment behind.
