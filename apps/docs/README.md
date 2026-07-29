# Axern Documentation Site

This package builds the public documentation at
[`axern.cofy-x.space`](https://axern.cofy-x.space) with Astro Starlight. The
site is static, includes Pagefind search, and publishes through Cloudflare
Pages.

## Content Boundary

`src/content/docs/` owns public user journeys: getting started, CLI, SDK,
Axrun, and concise platform concepts. English pages are normative. Files under
`src/content/docs/zh-cn/` translate selected pages; missing translations use
Starlight's explicit English fallback.

The repository root [`docs/`](../../docs/README.md) owns detailed engineering
architecture, maintainer operations, verification contracts, and product
design. Public pages link to those documents when readers need implementation
detail instead of duplicating them.

Homepage and shared visual work follows the repository's
[documentation site visual direction](../../docs/decisions/docs-site-visual-direction.md).

Shared styles use one stable entrypoint at `src/styles/custom.css`:

- `tokens.css` defines theme and Axern design tokens.
- `shell.css` adapts the Starlight shell and shared documentation elements.
- `home.css` owns the homepage visual system and responsive behavior.

Keep responsive and reduced-motion rules with the surface they affect. Add a
new stylesheet only when a new page-level surface has an independent visual
contract.

## Local Development

```bash
pnpm install --frozen-lockfile
make docs-dev
make docs-verify
```

`docs-verify` checks Astro types, Markdown, Mermaid syntax, required assets,
the production build, generated Pagefind data, and internal links.

The social preview is generated from the editable SVG source. Regenerate its
checked-in 1200x630 PNG whenever the source changes:

```bash
make docs-social-card
```

Regenerate the terminal GIFs from their checked-in VHS tapes with:

```bash
make docs-assets
```

The homepage Service recording is a separate data-plane acceptance artifact.
With the local Compose stack ready and a `compose` context configured, exercise
the same SDK and CLI flow without recording, or regenerate the GIF:

```bash
make docs-service-demo
make docs-service-asset
```

## Publication

The GitHub Actions workflow builds the same static output and deploys
`apps/docs/dist` with the pinned workspace Wrangler version. Configure the
GitHub environment `production` with `CLOUDFLARE_API_TOKEN` and
`CLOUDFLARE_ACCOUNT_ID`. Set the repository variable
`CLOUDFLARE_PAGES_PROJECT` when the Pages project is not named `axern-docs`.

Attach `axern.cofy-x.space` to that Pages project in Cloudflare. DNS and
account provisioning remain operator-owned and are not encoded in this public
repository.
