---
title: Coding Agents
description: Run Codex or Claude Code in a persistent Axern workspace while provider credentials stay on your machine.
---

`axern agent` manages a persistent remote coding workspace for Codex and
Claude Code. The workspace keeps one Service for compute, one Volume for
project data mounted at `/home/axern/workspace`, and a Tunnel for each
connection session. The agent bundle is resolved from the control-plane
catalog and mounted read-only; the default `coding-base` template selects the
workspace rootfs, not the agent executable.

The provider token never leaves your machine. The remote runtime receives only
a session-scoped local adapter token and a loopback base URL; the local
credential proxy holds the real upstream credential.

## Create a profile

Profiles live in the local CLI config under `agent_profiles`. A Codex upstream
must implement the OpenAI Responses API:

```bash
axern agent profile set dev-codex \
  --agent codex \
  --provider openai \
  --upstream https://api.openai.com/v1 \
  --token <local-token> \
  --model <model> \
  --use

axern agent profile set dev-claude \
  --agent claude-code \
  --provider anthropic \
  --upstream https://api.anthropic.com \
  --token <local-token> \
  --model <model>
```

Profile and doctor commands never print the stored token.

## Start a session

```bash
axern agent doctor --workspace project-a --profile dev-codex
axern agent shell --workspace project-a --profile dev-codex
```

The first session creates the workspace automatically: it starts or resumes
the Service, waits for a ready replica, opens the credential proxy and
Tunnel, and writes the remote agent configuration. When `--workspace` is
omitted, the selected profile name is used.

Run the configured agent CLI non-interactively, passing arguments after `--`:

```bash
axern agent run --workspace project-a --profile dev-codex -- exec --model <model> "reply ok only"
axern agent run --workspace project-a --profile dev-claude -- -p "reply ok only"
```

Use `connect` to keep the proxy, tunnel, and remote config active without
entering a shell.

## Suspend, switch, and delete

Ending a session only closes that connection. Suspend compute while retaining
the workspace data:

```bash
axern agent list
axern agent stop --workspace project-a
```

`stop` scales the Service to zero and is idempotent. The next `shell`, `run`,
or `connect` resumes the same Service and Volume. A running workspace accepts
only its active profile; to switch agents or models, suspend first and
reconnect with another profile.

Delete a suspended workspace only when its data is no longer needed:

```bash
axern agent stop --workspace project-a
axern agent workspace delete --workspace project-a --yes
```

Deletion waits for allocation release and physical volume reclaim. Recreating
the same workspace name produces new identities and an empty data directory.

:::note
`axern agent` is the interactive development path. Reproducible agent
execution with trajectories and evidence uses [Axrun](/axrun/) instead.
:::

For the complete workspace, security, and diagnostics contract, see the
repository's
[agent runtime document](https://github.com/cofy-x/axern/blob/main/apps/cli/docs/agent.md).
