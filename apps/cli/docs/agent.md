# Axern Agent Workspace

`axern agent` manages a persistent remote coding workspace. Its durable model
is deliberately small:

- Workspace is the project and data identity.
- Profile is the local agent, model, and provider-credential identity.
- Service is the workspace's compute identity.
- Volume is the workspace's persistent data identity.
- Tunnel is one connection session.

A workspace is identified by `--workspace`. When omitted, the selected profile
name is used. Names may contain lowercase letters, digits, `.`, `_`, and `-`,
must start with a letter or digit, and are limited to 48 characters.

Every workspace keeps one Service and mounts `agent-workspace-<workspace>` at
`/home/axern/workspace`. The local persistent volume pins recovery to its original
node. A missing node is an explicit storage-topology failure; Axern does not
replace the workspace with an empty directory.

The selected profile namespace determines the Service namespace when the
workspace is first created. That namespace then belongs to the workspace and
does not change during later profile switches; the new profile's Environment
is resolved in the existing workspace namespace.

Bootstrap makes only the mount root writable by the remote user. It first
tries to correct ownership and falls back to mount-root permission adjustment
for filesystems such as 9p that do not preserve `chown`. Existing content is
never changed recursively. If the expected mount is absent, bootstrap fails
instead of creating an empty workspace directory.

Starting `shell`, `run`, or `connect` creates or resumes the Service, waits for
one ready replica, opens a credential proxy and Tunnel, and writes the remote
agent configuration. Ending the connection only closes that session. Use
`stop` to suspend compute while retaining the Service and workspace data.

## Profiles

Profiles live in the normal Axern CLI config file under `agent_profiles`.
Provider tokens are stored locally and are not printed by profile or doctor
commands.

For both supported agents, the default `template_id` is `coding-base`. The
template selects the workspace rootfs, not the agent executable. `axern agent`
implicitly resolves the matching `codex` or `claude-code` bundle from the
control-plane catalog and mounts it read-only; profiles do not store bundle
image references. An explicit `--template-id` may select another compatible
workspace base, but it must satisfy the same shell and bundle ABI contract.

Create a Codex profile:

```bash
axern agent profile set dev-codex \
  --agent codex \
  --provider openai \
  --upstream https://api.example.test/v1 \
  --token-stdin \
  --model <model> \
  --use
```

Pipe the provider token through stdin or use `--token-env NAME`; provider
credentials are never accepted as command-line values.

The Codex upstream must implement the OpenAI Responses API. Include the API
base path, normally `/v1`, in `--upstream`; an endpoint that only implements
chat completions is not compatible with the current Codex CLI.
The profile stores this requirement as the typed `wire_api: responses`
contract. Axern does not translate Responses requests into Chat Completions.

For an upstream that must be reached directly from the local developer machine,
add `--agent-config upstream_no_proxy=true`. This setting only affects the
local proxy's upstream request; the remote runtime still talks to the tunnel
loopback endpoint.

Create a Claude Code profile:

```bash
axern agent profile set dev-claude \
  --agent claude-code \
  --provider anthropic \
  --upstream https://api.example.test/anthropic \
  --token-env AXERN_ANTHROPIC_TOKEN \
  --model <model>
```

Manage profiles:

```bash
axern agent profile list
axern agent profile get dev-codex
axern agent profile use dev-codex
```

Inspect and suspend workspaces:

```bash
axern agent list
axern agent list --workspace project-a
axern agent stop --workspace project-a
```

`stop` scales the Service to zero and is idempotent for an already suspended
workspace. The next `shell`, `run`, or `connect` resumes the same Service and
Volume with a new Allocation. `agent list` reports the stable lifecycle values
`starting`, `running`, `suspended`, `deleting`, and `degraded`.

A running workspace only accepts its active profile. To switch agents or
models, suspend it and reconnect with another profile:

```bash
axern agent stop --workspace project-a
axern agent shell --workspace project-a --profile dev-claude
```

The profile switch updates the suspended Service's Environment and labels. It
does not replace the Service or Volume.

Permanently delete a suspended workspace only when its data is no longer
needed:

```bash
axern agent stop --workspace project-a
axern agent workspace delete --workspace project-a
# Non-interactive automation:
axern agent workspace delete --workspace project-a --yes --timeout 10m
```

Interactive deletion requires typing the complete workspace name. Automation
must pass `--yes`. The command waits for allocation release and physical volume
reclaim. A timeout stops only the local wait; repeat the same command to keep
waiting. Completed deletion is idempotent, while a name that never existed is
reported as not found. Recreating the same workspace produces new Service and
Claim identities and an empty data directory.

Idle suspend, snapshots, and cross-node recovery are not part of this contract.

## Daily Operations

Check a profile and start a session:

```bash
axern agent profile list
axern agent doctor --workspace project-a --profile dev-codex
axern agent shell --workspace project-a --profile dev-codex
```

The shell starts in the remote agent workspace, which is created automatically
for the session.

Run the configured agent CLI non-interactively:

```bash
axern agent run --workspace project-a --profile dev-codex -- exec --model <model> "reply ok only"
axern agent run --workspace project-a --profile dev-claude -- -p "reply ok only"
```

Open the configured agent CLI interactively:

```bash
axern agent run --workspace project-a --profile dev-codex
```

Keep the proxy, tunnel, and remote config active without entering a shell:

```bash
axern agent connect --workspace project-a --profile dev-codex
```

Use `codex` profiles with Codex CLI arguments and `claude-code` profiles with
Claude CLI arguments.

## Security Model

The remote runtime receives only a session-scoped local adapter token and a
loopback base URL. The provider token stays in the local profile and local
proxy. It is never written to the Service, Volume, remote configuration, logs,
or JSON output.

`axern agent` is for interactive development runtimes. Axrun rollout and
telemetry flows use sandboxd managed proxy instead.

## Diagnostics

Diagnose the selected profile:

```bash
axern agent doctor --workspace project-a --profile dev-codex
```

Doctor validates the profile, calls the configured provider with a minimal
request, and then checks the workspace template, agent bundle catalog entry,
mounted image and binary path, and reusable service. The probe
uses `--model` when provided and otherwise uses the profile's configured
model. It reports protocol, authentication, model, rate-limit, timeout, and
availability failures without printing provider credentials.

Interactive `agent shell`, `agent run`, and `agent connect` do not issue an
extra provider request. This keeps shell-based diagnosis available during a
provider outage and avoids an additional billable request for every session.

Doctor reports the resolved agent, provider, runtime type, profile validity,
and the supported approval policies for interactive local and isolated Axern
execution. It never prints the provider token or upstream credentials.
