# axern CLI

`axern` is the product CLI for Axern platform resources and interactive
development. It talks to public APIs through gatewayd; node-local operations
belong to subsystem tools such as `axctl`.

## Product Boundary

- `axern` manages contexts, namespaces, environments, runs, services,
  functions, quotas, secrets, tunnels, SSH sessions, interactive agents, and
  audited admin workflows.
- SDKs are the explicit programmatic interface.
- `axrun` owns reproducible agent rollout planning, execution, validation, and
  trajectory export.

The command path is:

```text
cliapp -> commands -> application -> public SDK clients -> gatewayd
```

The dashboard also reads reliability state through gateway RPC. It does not
connect to controld debug HTTP endpoints.

## Contexts

The default context file is `~/.config/axern/config.json`. Override it with
`--config` or `AXERN_CONFIG`; select a context with `--context` or
`AXERN_CONTEXT`.

```json
{
  "current_context": "hk",
  "contexts": {
    "hk": {
      "endpoint": "gateway.example.com:443",
      "service_url": "https://services.example.com",
      "ssh_endpoint": "gateway.example.com:22",
      "ssh_identity_file": "~/.ssh/axern_hk",
      "tls": {
        "ca_cert": "/path/to/ca.crt",
        "cert": "/path/to/client.crt",
        "key": "/path/to/client.key",
        "server_name": "gateway.example.com"
      },
      "proxy_mode": "direct"
    }
  }
}
```

`proxy_mode` is `env` or `direct`. API and tunnel traffic share `endpoint`,
TLS, and proxy policy. SSH uses the same context but its own endpoint and key.

Direct overrides use `AXERN_ENDPOINT`, `AXERN_SERVICE_URL`,
`AXERN_SSH_ENDPOINT`, `AXERN_SSH_IDENTITY_FILE`, `AXERN_TLS_CA_CERT`,
`AXERN_TLS_CERT`, `AXERN_TLS_KEY`, `AXERN_TLS_SERVER_NAME`, and
`AXERN_PROXY_MODE`.

## Commands

Canonical resource names are used in documentation. Product aliases are
limited to `ctx`, `ns`, `svc`, and `fn`.

```bash
axern context list
axern context use hk
axern context import-kubernetes local --namespace axern-system --current
axern doctor
axern doctor --probe

axern local up
axern local status
axern run --file run.yaml
axern run python:3.12-slim -- python -c 'print("hello")'
axern run --detach python:3.12-slim -- python -c 'print("later")'
axern run logs --follow <run-id>
axern service create --file service.yaml --wait
axern service get <service-id>
axern service delete <service-id> --wait
axern function deploy --file function.yaml --wait
axern function invocation list --namespace default <function-name>
axern quota get --namespace default

axern ssh <allocation-id|service-id>
axern service tunnel <service-id> --to 127.0.0.1:8080
axern tunnel doctor --service-id <service-id>

axern agent shell --workspace <workspace> --profile <profile>
axern agent run --workspace <workspace> --profile <profile> -- exec --model <model> "reply ok only"
axern agent stop --workspace <workspace>
axern agent workspace delete --workspace <workspace> --yes
axern dashboard

axern admin reliability check
axern admin consistency check
axern admin node list --status active
axern admin node retire <node-id> --operator-reason "host permanently removed"
axern admin service purge <service-id> --operator-reason "expired test resource"
axern identity whoami
axern admin principal list
axern admin role-binding list --namespace default
```

Service deletion is asynchronous. `service delete` reports that deletion was
requested; add `--wait` to wait for the persisted deletion phase to complete.
The default `service list` view excludes deleted audit records; use
`service list --status deleted` or exact `service get <service-id>` to inspect
them until an administrator explicitly purges the record.

`agent` requires an explicit `shell`, `run`, `connect`, `doctor`, `list`,
`stop`, `workspace`, or `profile` subcommand. Agent workspaces keep one Service
and persistent Volume; `stop` scales compute to zero and the next session
resumes it. `agent workspace delete` permanently reclaims a suspended
workspace. `service
get` includes rollout and latest event state. `quota get` includes quota,
usage, and admission signals.

Generate completion with `axern completion bash|zsh|fish`.

## Platform Doctor

`axern doctor` is read-only by default. It validates local connection settings,
mTLS material and certificate lifetime, gateway connectivity, the authenticated
Principal, authorization for the selected namespace, and the runtime catalog.
Messages and JSON output use stable codes
and do not include certificate paths, private keys, raw endpoints, or server
error text.

Use `--probe` when a real data-plane check is required:

```bash
axern doctor --namespace default --probe
```

The probe creates a catalog-backed Environment from the `python311` template,
executes a small `runsc` Run, and deletes the temporary Environment. The Run
remains as normal control-plane history. Use `--template-id`,
`--runtime-class`, and `--probe-timeout` only with `--probe`.

## Local DNS Doctor

`axern local doctor` uses the same `status`, `mode`, and stable check result
shape as the platform doctor. It validates the resolver configuration actually
materialized for an initialized stack and, while the stack is running, queries
those resolvers directly from the Node container. These checks are read-only.

Use the explicit probe to verify the normal Environment and Run path in a real
OCI sandbox:

```bash
axern local doctor --probe
```

The default query is `axern.cofy-x.space.`. Use `--dns-query-name` for a
managed or private domain. The override is stored in a temporary Secret and is
not included in Run arguments, doctor JSON details, or probe output. The probe
creates a temporary Namespace, Secret, Environment, and Run. Cleanup cancels
an active Run, then deletes the Environment, Secret, and Namespace even after
failure or cancellation; the terminal Run remains as normal control-plane
history. The probe always uses the product-owned `local` context, regardless
of the currently selected context.

Read-only checks default to 15 seconds (`--check-timeout`); sandbox execution
defaults to five minutes (`--probe-timeout`). `--template-id` defaults to
`python311`, and `--runtime-class` defaults to `runsc`; all three sandbox-only
options require `--probe`.

## Resource Spec

Run, Service, and Function creation accepts a strict YAML or JSON envelope:

```yaml
api_version: axern/v1
kind: Run
metadata:
  namespace: default
  labels: {}
spec:
  source:
    template: python311
  command:
    argv: [python, -c, "print('ok')"]
  runtime_class: runsc
  resources: {}
```

`spec.source` selects exactly one environment, template, or image. Unknown
fields, conflicting sources, invalid quantities, invalid probes, and a kind
that does not match the command are rejected. Function source directories are
resolved relative to the spec file and cannot escape through `..` or symlinks.

When `--file` is used, resource-definition flags cannot be mixed with the
spec. Context, output, detach, and timeout flags remain operational overrides.

## Output And Exit Codes

Interactive output defaults to `table`. Automation uses `--output json`; JSON
is rendered from stable public DTOs rather than generated proto objects. YAML
is an input format, not an output format.

- `0`: success.
- `1`: platform, network, or server operation failure; `doctor` also uses it
  for a degraded result.
- `2`: invalid arguments, context, configuration, or spec; `doctor` still
  renders its structured configuration check before returning this code.
- `3`: `doctor` could not complete a required health check.
- `run`: a normally terminated workload returns its exit code; the first
  interrupt requests cancellation and a second exits immediately with `130`.

## Build And Verify

From the repository root:

```bash
make axern-cli-build
go test ./apps/cli/...
go vet ./apps/cli/...
make axern-cli-check-architecture
make local-compose-refresh-verify
```

See [tunnel usage](./docs/tunnel.md), [agent runtime](./docs/agent.md), and the
[resource model](../../docs/architecture/resource-model.md) for deeper product
contracts.
