# axern CLI

`axern` is the product CLI for Axern platform resources and interactive development. It talks to public APIs through gatewayd; node-local operations belong to subsystem tools such as `axctl`.

## Product Boundary

- `axern` manages contexts, namespaces, environments, runs, quotas, secrets, tunnels, SSH sessions, interactive terminals, and audited admin workflows.
- SDKs are the explicit programmatic interface.

The command path is:

```text
cliapp -> commands -> application -> public SDK clients -> gatewayd
```

## Contexts

The default context file is `~/.config/axern/config.json`. Override it with `--config` or `AXERN_CONFIG`; select a context with `--context` or `AXERN_CONTEXT`.

```json
{
  "current_context": "hk",
  "contexts": {
    "hk": {
      "endpoint": "gateway.example.com:443",
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

`proxy_mode` is `env` or `direct`. API and tunnel traffic share `endpoint`, TLS, and proxy policy. SSH uses the same context but its own endpoint and key.

Direct overrides use `AXERN_ENDPOINT`, `AXERN_SSH_ENDPOINT`, `AXERN_SSH_IDENTITY_FILE`, `AXERN_TLS_CA_CERT`, `AXERN_TLS_CERT`, `AXERN_TLS_KEY`, `AXERN_TLS_SERVER_NAME`, and `AXERN_PROXY_MODE`.

## Commands

Canonical resource names are used in documentation. Product aliases are limited to `ctx` and `ns`.

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
axern quota get --namespace default

axern ssh <allocation-id>
axern tunnel open --allocation-id <allocation-id> --local 127.0.0.1:8080
axern tunnel doctor --allocation-id <allocation-id>

axern admin reliability check
axern admin consistency check
axern admin node list --status active
axern admin node revoke <node-id> --operator-reason "host authority compromised"
axern admin node retire <node-id> --operator-reason "host permanently removed"
axern identity whoami
axern admin principal list
axern admin role-binding list --namespace default
```

`quota get` includes quota, usage, and admission signals.

Generate completion with `axern completion bash|zsh|fish`.

## Platform Doctor

`axern doctor` is read-only by default. It validates local connection settings, mTLS material and certificate lifetime, gateway connectivity, the authenticated Principal, and authorization for the selected namespace. Messages and JSON output use stable codes and do not include certificate paths, private keys, raw endpoints, or server error text.

Use `--probe` when a real data-plane check is required:

```bash
axern doctor --namespace default --probe
```

The probe creates an Environment from the explicit OCI image passed with `--image`, executes a small Run, and deletes the temporary Environment. The Run remains as normal control-plane history. Use `--image` and `--probe-timeout` only with `--probe`.

## Local DNS Doctor

`axern local doctor` uses the same `status`, `mode`, and stable check result shape as the platform doctor. On native Linux, initialization snapshots the first resolver file that contains usable non-loopback addresses, falling back from the systemd-resolved stub to `/run/systemd/resolve/resolv.conf`; Docker Desktop keeps Node-derived resolver discovery. While the stack is running, the doctor queries every effective resolver directly from the Node container. `axern local up` performs the same Node check before reporting ready. These checks are read-only.

Use the explicit probe to verify the normal Environment and Run path in a real OCI sandbox:

```bash
axern local doctor --probe
```

The default query is `axern.cofy-x.space.`. Use `--dns-query-name` for a managed or private domain. The override is stored in a temporary Secret and is not included in Run arguments, doctor JSON details, or probe output. The probe creates a temporary Namespace, Secret, Environment, and Run. Cleanup cancels an active Run, then deletes the Environment, Secret, and Namespace even after failure or cancellation; the terminal Run remains as normal control-plane history. The probe always uses the product-owned `local` context, regardless of the currently selected context.

Read-only checks default to 15 seconds (`--check-timeout`); sandbox execution defaults to five minutes (`--probe-timeout`). The probe requires an explicit `--image`; both sandbox-only options require `--probe`.

## Resource Spec

Run creation accepts a strict YAML or JSON envelope:

```yaml
api_version: axern/v1
kind: Run
metadata:
  namespace: default
  labels: {}
spec:
  source:
    image: docker.io/library/python:3.12-slim
  command:
    argv: [python, -c, "print('ok')"]
  resources: {}
```

`spec.source` selects exactly one existing environment or OCI image. Unknown fields, conflicting sources, invalid quantities, invalid probes, and a kind that does not match the command are rejected.

When `--file` is used, resource-definition flags cannot be mixed with the spec. Context, output, detach, and timeout flags remain operational overrides.

## Output And Exit Codes

Interactive output defaults to `table`. Automation uses `--output json`; JSON is rendered from stable public DTOs rather than generated proto objects. YAML is an input format, not an output format.

- `0`: success.
- `1`: platform, network, or server operation failure; `doctor` also uses it for a degraded result.
- `2`: invalid arguments, context, configuration, or spec; `doctor` still renders its structured configuration check before returning this code.
- `3`: `doctor` could not complete a required health check.
- `run`: a normally terminated workload returns its exit code; the first interrupt requests cancellation and a second exits immediately with `130`.

## Build And Verify

From the repository root:

```bash
make axern-cli-build
go test ./apps/cli/...
go vet ./apps/cli/...
make axern-cli-check-architecture
make local-compose-refresh-verify
```

See [tunnel usage](./docs/tunnel.md) and the [resource model](../../docs/architecture/resource-model.md) for deeper product contracts.

## Deployment Identity

`axern admin pki bootstrap --directory <private-directory> --cluster <trust-domain>` initializes local signing authority and service identities without contacting a cluster. Publish the public/role material and the separate controld-only signer as described in the [Kubernetes guide](../docs/src/content/docs/getting-started/kubernetes.md). The command never generates Node keys; `--renew-services` explicitly renews service certificates without replacing CA or administrator identity.

Admit each Node with `axern admin node admit <node-id> --enrollment-token-file <file> --operator-reason <reason>`. Registration consumes that token for one CSR within one hour. Normal node calls authenticate the node-owned certificate and automatic renewal keeps the same Node identity. The old credential flag and shared Node certificate are removed; internal Proto changes require matching node/control images and a rebuilt local database.

Node revocation withdraws authority without declaring resources cleaned; safe retirement remains blocked until execution and access obligations converge. Use `node list --status revoked` and `admin audit list --operation revoke-node` for incident diagnosis. Node identity is explicit and independent of the Kubernetes hostname.
