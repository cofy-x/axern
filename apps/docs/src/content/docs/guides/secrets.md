---
title: Secrets
description: Store immutable, metadata-only-listed secrets for registry credentials and workload configuration.
---

`axern secret` stores immutable secrets in a namespace. Secrets are referenced
by ID from other resources — for example an environment's
`--registry-credential-id` — and are never embedded in resource specs. Listing
and inspection expose metadata only, not secret values.

## Create a secret

Opaque secrets take one or more `key=value` entries. Prefer stdin so values do
not appear in command arguments. Reference an existing environment variable
instead of writing the value literally in a command or heredoc:

```bash
printf '%s\n' "API_KEY=$AXERN_SECRET_API_KEY" | \
  axern secret create \
    --namespace default \
    --literal-stdin \
    --label team=runtime
```

The stdin format accepts one `KEY=VALUE` entry per line. Blank lines are
ignored, values are preserved verbatim, and the JSON-encoded `string_data` map
must not exceed 64 KiB. The CLI intentionally does not accept opaque secret
values as command-line arguments.

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

Secrets are immutable. Rotate them as a replacement workflow:

1. Create a new Secret with the replacement value.
2. Update or replace every resource that references the old Secret ID.
3. Wait for the new Service, Function, Run, or Environment revision to become
   ready and verify the workload.
4. Delete the old Secret only after no active resource references it.

For an Environment registry credential, create a new Environment because the
Environment itself is immutable; then point the workload at the new
Environment. For `secret-env` and `secret-file` projections, update the
Service or Function specification and let its immutable revision roll out.

:::note
Secrets hold platform credential material such as registry pulls. Agent
provider tokens have dedicated stores that keep plaintext out of generic
APIs: local `axern agent` profiles for interactive workspaces, and versioned
[Axrun profiles](/axrun/) for managed rollouts.
:::
