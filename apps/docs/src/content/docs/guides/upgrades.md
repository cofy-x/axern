---
title: Upgrades and Versioning
description: Keep the CLI, local stack, Helm chart, and SDKs on one coherent Axern version.
---

Axern publishes the CLI, Helm chart, runtime images, and all three SDKs under one repository version. Treat a pre-1.0 release as one coherent unit: mixed versions are not a supported combination.

## v0.7.0 clean-state boundary

Upgrading from v0.6.2 to v0.7.0 requires fresh control-plane and node-local state, not an in-place Helm upgrade. Export required output, stop workloads and confirm cleanup before replacing state. Recreate Node enrollment and Principal Credentials using the [Kubernetes installation guide](/getting-started/kubernetes/). Review the [v0.7.0 release notes](https://github.com/cofy-x/axern/blob/main/docs/releases/v0.7.0.md) for removed APIs, coordinated component versions and rollback requirements. Do not erase recovery records beneath running sandboxes or use candidate versions before publication completes.

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

## Replace Local Axern

The CLI never silently changes a running local stack and does not carry local database migrations across versions. Export required outputs, then rebuild the local instance explicitly:

```bash
axern local reset --force
axern local up
```

This deliberately replaces local data and identity material. See the [Local Axern reference](/guides/local/) for the full lifecycle.

## Upgrade a Kubernetes install

For a release that explicitly supports retaining the existing state, pin the chart to the same release as the CLI and review your operator-owned values against the new chart. Do not carry old image tags or removed settings forward. The following command is not the v0.6.2-to-v0.7.0 upgrade procedure:

```bash
helm upgrade axern oci://ghcr.io/cofy-x/charts/axern \
  --version <version> \
  --namespace axern-system \
  -f values.yaml \
  --wait \
  --timeout 15m
```

The chart's default images are immutable version tags from the same release. Ensure image overrides in your values also select that release.

## Pin the SDKs

Match the SDK package version to the Axern release and commit your lockfile or resolved `go.mod` for repeatable builds:

- Python: `uv add axern-sdk==<version>`
- Go: `go get github.com/cofy-x/axern/sdk/go@<version>`
- TypeScript: `pnpm add @cofy-x/axern-sdk@<version>`

Do not use a floating `latest` dependency in production; the SDK and control-plane contracts are versioned together.

## Pre-1.0 expectations

Before 1.0, minor releases may change public commands, specs, and SDK surfaces. Read the curated release notes in the repository's [`docs/releases/`](https://github.com/cofy-x/axern/tree/main/docs/releases) when upgrading across minor versions, and rerun `axern doctor --namespace default` against the upgraded platform before resuming workloads.
