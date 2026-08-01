---
title: SSH Access
description: Open an SSH-compatible terminal into an allocation or a ready service replica.
---

`axern ssh` opens an interactive terminal into a running allocation or a ready
service replica through the gateway's SSH edge. It uses the local OpenSSH
client and the SSH endpoint and identity file from the selected context.

:::caution[SSH is an explicit trust boundary]

SSH is disabled by the default Helm installation. Enable it only when an
interactive workflow needs it, configure an authorized public key, and keep
the private identity file restricted. SSH uses its own gateway-wide
`authorized_keys` boundary; it does not inherit Principal or namespace RBAC.
Anyone holding an authorized identity can request a shell for any allocation
ID that the gateway can resolve.

:::

```bash
axern ssh <allocation-id>
axern ssh <service-id>
```

When the target is a service, the CLI selects a ready replica; pass
`--allocation-id` to choose a specific one. Run a one-off command instead of
an interactive shell by appending it after the target:

```bash
axern ssh <service-id> -- uname -a
axern ssh <allocation-id> --shell /bin/sh
```

Useful flags:

- `--user <name>`: container user for the session.
- `--identity-file <path>` / `--ssh-endpoint <host:port>`: override the
  context connection, also configurable with `AXERN_SSH_IDENTITY_FILE` and
  `AXERN_SSH_ENDPOINT`.
- `--ssh-option <option>`: pass an extra OpenSSH option; may be repeated.
- `--strict-host-key-checking`: enforce the local `known_hosts` policy. The
  default is relaxed checking for ephemeral sandbox hosts; use strict checking
  for shared deployments and review host-key rotation before trusting a new
  gateway.

For a persistent coding workspace with an agent preinstalled, prefer
[`axern agent shell`](/guides/agent/); for reaching a local TCP service from
inside the allocation, use a [reverse tunnel](/guides/tunnels/).
