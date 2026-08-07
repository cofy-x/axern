---
title: Upgrades and Versioning
description: Keep the CLI, local stack, Helm chart, and SDKs on one coherent Axern version.
---

Axern publishes the CLI, Helm chart, runtime images, and all three SDKs under
one repository version. Treat a pre-1.0 release as one coherent unit: mixed
versions are not a supported combination.

## Upgrade the CLI

With Homebrew:

```bash
brew upgrade axern
```

With the shell installer, rerun it and pin the version explicitly:

```bash
curl -fsSL https://raw.githubusercontent.com/cofy-x/axern/main/install.sh \
  | AXERN_VERSION=<version> sh
```

## Upgrade Local Axern

The CLI never silently changes a running local stack. When
`axern local status` reports a version mismatch, migrate explicitly:

```bash
axern local upgrade
```

The upgrade stops the old stack, creates a timestamped backup of data,
identities, metadata, and deployment files, applies the supported migration,
and verifies health. Downgrades are rejected; see the
[Local Axern reference](/guides/local/) for the full lifecycle.

## Upgrade a Kubernetes install

Pin the chart to the same release as the CLI and reuse your values:

```bash
helm upgrade axern oci://ghcr.io/cofy-x/charts/axern \
  --version <version> \
  --namespace axern-system \
  -f values.yaml \
  --reuse-values \
  --wait \
  --timeout 15m
```

The chart's default images are immutable version tags from the same release,
so chart and workloads move together.

## Pin the SDKs

Match the SDK package version to the Axern release and commit your lockfile or
resolved `go.mod` for repeatable builds:

- Python: `uv add axern-sdk==<version>`
- Go: `go get github.com/cofy-x/axern/sdk/go@<version>`
- TypeScript: `pnpm add @cofy-x/axern-sdk@<version>`

Do not use a floating `latest` dependency in production; the SDK and
control-plane contracts are versioned together.

## Pre-1.0 expectations

Before 1.0, minor releases may change public commands, specs, and SDK
surfaces. Read the curated release notes in the repository's
[`docs/releases/`](https://github.com/cofy-x/axern/tree/main/docs/releases)
when upgrading across minor versions, and rerun
`axern doctor --namespace default` against the upgraded platform before
resuming workloads.
