---
title: Upgrades and Versioning
description: Keep the CLI, local stack, Helm chart, and SDKs on one coherent Axern version.
---

Axern publishes the CLI, Helm chart, runtime images, and all three SDKs under one repository version. Treat each release as one coherent unit: mixed versions are not a supported combination.

## Before an upgrade

Read the target release's [release notes](https://github.com/cofy-x/axern/tree/main/docs/releases) before changing any component. The notes define whether state can be retained, which contracts changed, and whether the release requires a clean deployment. Export required outputs and confirm that workloads and cleanup have converged before replacing state. Never erase node recovery records beneath running sandboxes, and do not use candidate versions before publication completes.

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

When the target release explicitly supports retaining existing state, pin the chart to the same release as the CLI and review operator-owned values against the new chart. Do not carry old image tags or removed settings forward. When retention is not supported, export required outputs, stop workloads, confirm cleanup, and follow the target release's clean-deployment procedure instead of running an in-place upgrade.

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

## Compatibility expectations

Public compatibility is defined by the target release notes, not by internal database tables, protobuf packages, or node-local files. Read the curated notes when upgrading across versions, and rerun `axern doctor --namespace default` against the upgraded platform before resuming workloads.
