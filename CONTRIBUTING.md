# Contributing to Axern

Thank you for helping improve Axern. Start with an issue for changes that alter
public APIs, security boundaries, durable state, or component ownership.

## Development

Read the root [Agent Contract](./AGENTS.md), then the nearest module contract
and README for the area you are changing. Keep changes focused and use the root
Makefile as the stable command surface.

```bash
make bootstrap
make build
make test
make lint
make proto-generated-check
make agent-doc-check
make open-source-check
```

Integration-sensitive changes must also run the relevant Compose, kind, or
runtime smoke documented by the owning module. Pull requests should describe
the user impact, design tradeoffs, verification performed, and any operational
risk.

## Commit Style

Use Conventional Commits such as `feat:`, `fix:`, `docs:`, `test:`,
`refactor:`, or `chore:`. Keep generated files and their source changes in the
same commit.

## Developer Certificate of Origin

Axern uses the [Developer Certificate of Origin 1.1](./DCO), not a contributor
license agreement. Sign off every commit:

```bash
git commit -s -m "fix: describe the change"
```

The sign-off certifies that you have the right to submit the contribution under
the project's Apache-2.0 license.

## Security

Do not report vulnerabilities in public issues. Follow
[the security policy](./SECURITY.md).
