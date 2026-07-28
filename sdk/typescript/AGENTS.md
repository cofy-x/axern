# AGENTS.md

## Purpose

This contract applies to the Axern TypeScript SDK.

## Design Rules

- Keep this SDK Node.js-first until browser support is explicitly designed.
- Keep SDK code as a thin wrapper over Axern control and node RPCs. Do not add
  SDK-side shell fallbacks for platform file/process behavior.
- Keep generated or dynamic proto access isolated under `src/generated`; product
  APIs should live in `client`, `sandbox`, `node`, `errors`, and small shared
  type modules.
- Dynamic proto loading is acceptable while the SDK surface is still changing.
  Prefer a deliberate static-stub migration once the public API stabilizes.
- Preserve clear names for image inputs: user-provided refs may be tag/name/digest,
  while resolved refs are digest-pinned values.
- Prefer Promise and async-iterator APIs for TypeScript ergonomics.

## Validation

Run these from the repository root after SDK changes:

```bash
make sdk-typescript-verify
```

When local compose is running and the change touches real RPC behavior, also run:

```bash
pnpm --filter @cofy-x/axern-sdk run smoke:local
```
