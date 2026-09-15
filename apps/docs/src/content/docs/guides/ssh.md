---
title: SSH Access
description: Open an SSH-compatible terminal into a running allocation.
---

`axern ssh` opens an interactive terminal into a running allocation through the gateway's SSH edge. It uses the local OpenSSH client and the SSH endpoint and identity file from the selected context.

:::caution[SSH is an explicit trust boundary]

SSH is disabled by the default Helm installation. Enable it when an interactive workflow needs it and configure a persistent gateway host key. Register each client public key as a Principal Credential with an explicit expiry using `axern admin credential add --ssh-public-key <file> --expires-at <RFC3339> <principal-id>`. Existing namespace roles must permit execution on the target Allocation. Keep the private identity file restricted. Credential revocation and namespace role changes are rechecked during active sessions; inability to confirm authorization closes access without cancelling the Allocation.

:::

```bash
axern ssh <allocation-id>
```

Run a one-off command instead of an interactive shell by appending it after the target:

```bash
axern ssh <allocation-id> -- uname -a
axern ssh <allocation-id> --shell /bin/sh
```

Useful flags:

- `--user <name>`: container user for the session.
- `--identity-file <path>` / `--ssh-endpoint <host:port>`: override the context connection, also configurable with `AXERN_SSH_IDENTITY_FILE` and `AXERN_SSH_ENDPOINT`.
- `--ssh-option <option>`: pass an extra OpenSSH option; may be repeated.
- `--strict-host-key-checking`: enforce the local `known_hosts` policy. The default is relaxed checking for ephemeral sandbox hosts; use strict checking for shared deployments and review host-key rotation before trusting a new gateway.

For reaching a local TCP service from inside the allocation, use a [reverse tunnel](/guides/tunnels/).
