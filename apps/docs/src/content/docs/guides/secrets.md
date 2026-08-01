---
title: Secrets
description: Store immutable, metadata-only-listed secrets for registry credentials and workload configuration.
---

`axern secret` stores immutable secrets in a namespace. Secrets are referenced
by ID from other resources — for example an environment's
`--registry-credential-id` — and are never embedded in resource specs. Listing
and inspection expose metadata only, not secret values.

## Create a secret

Opaque secrets take one or more `key=value` literals:

```bash
axern secret create \
  --namespace default \
  --literal API_KEY=<value> \
  --label team=runtime
```

A Docker registry credential uses the `docker-config-json` type with a local
config file:

```bash
axern secret create \
  --namespace default \
  --type docker-config-json \
  --file ~/.docker/config.json
```

## Inspect and delete

```bash
axern secret list --namespace default
axern secret get <secret-id>
axern secret delete <secret-id>
```

Secrets are immutable: rotate by creating a new secret, updating the resources
that reference the old ID, and deleting the old one.

:::note
Secrets hold platform credential material such as registry pulls. Agent
provider tokens have dedicated stores that keep plaintext out of generic
APIs: local `axern agent` profiles for interactive workspaces, and versioned
[Axrun profiles](/axrun/) for managed rollouts.
:::
