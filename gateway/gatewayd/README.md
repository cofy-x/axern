# gatewayd

`gatewayd` is Axern's external entry point for public control API traffic,
allocation-bound browser terminal and SSH-compatible sessions, artifact and
sandbox operations, and foreground tunnel client peers.

It does not own placement, lifecycle, or durable state. It resolves explicit
Allocation targets through `controld`, then forwards traffic directly to the
selected `axnoded` node using attempt-scoped execution leases.

External CLI and SDK control-plane gRPC traffic should terminate at
`gatewayd`'s control edge listener, which is enabled by default. `controld`
stays private inside the cluster; `gatewayd` verifies external client mTLS and
forwards public control RPCs to the internal `controld` target with the
dedicated `gatewayd` certificate. Caller-supplied internal identity metadata is
discarded; gatewayd injects only the fingerprint of the leaf certificate it
verified. Controld resolves that fingerprint to a durable Principal and applies
platform or namespace role bindings on every RPC.

Tunnel foreground clients use the same public control edge. `gatewayd`
registers `axern.tunnel.v1.TunnelRelay`, resolves the session-bound internal
relay target through the private `GatewayControl` service, and forwards only
client peers to `tunneld`. Peer authentication remains bound to the tunnel
session token; gatewayd does not bypass it or treat the data stream as a public
resource-management RPC.
Node peers continue to connect directly to internal `tunneld` targets.

## Run

```bash
go run ./gateway/gatewayd \
  -http-address 127.0.0.1:25080 \
  -control-edge-address 127.0.0.1:25000 \
  -control-edge-tls-ca-cert .dev/certs/ca.crt \
  -control-edge-tls-cert .dev/certs/gatewayd.crt \
  -control-edge-tls-key .dev/certs/gatewayd.key \
  -control-target 127.0.0.1:24000 \
  -tls-ca-cert .dev/certs/ca.crt \
  -tls-cert .dev/certs/gatewayd.crt \
  -tls-key .dev/certs/gatewayd.key \
  -dev-token axern-local-dev
```

Enable the optional SSH-compatible terminal listener by also providing a
persistent host key and an `authorized_keys` file:

```bash
go run ./gateway/gatewayd \
  -http-address 127.0.0.1:25080 \
  -control-edge-address 127.0.0.1:25000 \
  -control-edge-tls-ca-cert .dev/certs/ca.crt \
  -control-edge-tls-cert .dev/certs/gatewayd.crt \
  -control-edge-tls-key .dev/certs/gatewayd.key \
  -ssh-enabled \
  -ssh-address 127.0.0.1:25022 \
  -ssh-host-key .dev/ssh/gateway_host_ed25519 \
  -ssh-authorized-keys .dev/ssh/authorized_keys \
  -control-target 127.0.0.1:24000 \
  -tls-ca-cert .dev/certs/ca.crt \
  -tls-cert .dev/certs/gatewayd.crt \
  -tls-key .dev/certs/gatewayd.key \
  -dev-token axern-local-dev
```

## Routes

- `GET /healthz`
- `/terminal/allocation/{allocation_id}` opens a WebSocket terminal and requires the dev token

Gateway routes are allocation-bound. The retired
`/svc/{namespace}/{service_id}/{port}/...` route has no compatibility handler.
## SSH Terminal

When SSH is enabled, `ssh <allocation_id>@<gateway-host> -p <ssh-port>` opens
an interactive `/bin/sh` session in the allocation through the same
allocation-scoped lease and `axnoded` `ExecStream` path used by the browser
terminal. Use `ssh -t <allocation_id>@<gateway-host> -p <ssh-port> /bin/bash`
to request a different interactive shell. Container users can be selected by
sending `AXERN_EXEC_USER` in the SSH environment; the `axern ssh --user` command
sets this for the common CLI path. The gateway terminates SSH; containers do
not need to run `sshd`.

The SSH surface supports interactive `shell` sessions and non-interactive
`exec` commands. Shell exec requests such as `/bin/bash` or `/bin/bash -l` are
started directly; arbitrary exec requests run through `/bin/sh -lc` without a
TTY unless the client requested one. It does not support SFTP, SCP, SSH agent
forwarding, X11 forwarding, or SSH TCP forwarding.

## Observability

Gateway metrics, traces, and logs use the shared OpenTelemetry pipeline. Domain
metrics cover allocation target resolution, upstream failures, lease retries,
tunnel relay traffic, and active terminal or SSH sessions. Standard Go runtime
metrics report heap, allocation, GC, goroutine, and scheduler behavior for
long-running stability analysis.
The deployment Prometheus scrapes the OTel Collector; gatewayd does not expose
a separate production metrics endpoint.

Every request emits a structured access log with method, path, route type,
status, duration, namespace, allocation id, node id, and error class. Logs and
metrics never include plaintext lease tokens,
Authorization headers, or terminal stdin/stdout content.

## Terminal Protocol

Clients send JSON text messages:

- `{"type":"stdin","data":"echo ok\n"}`
- `{"type":"resize","cols":120,"rows":40}`
- `{"type":"ping"}`
- `{"type":"close_stdin"}`

The server sends JSON text messages:

- `{"type":"stdout","data":"..."}`
- `{"type":"stderr","data":"..."}`
- `{"type":"exit","exit_code":0,"message":"..."}`
- `{"type":"error","message":"..."}`
- `{"type":"pong"}`

`exit_code:-1` means the runtime finished the interaction but could not report a
precise process exit status.

## Limits

Key flags/env:

- `-control-edge-address`
- `-control-edge-tls-ca-cert`, `-control-edge-tls-cert`, `-control-edge-tls-key`
- `-tunnel-relay-target`
- `-tunnel-relay-tls-ca-cert`, `-tunnel-relay-tls-server-name`
- `-read-header-timeout`, `-read-timeout`, `-write-timeout`, `-idle-timeout`
- `-terminal-idle-timeout`, `-terminal-max-duration`, `-terminal-max-message-bytes`
- `-ssh-enabled`, `-ssh-address`, `-ssh-host-key`, `-ssh-authorized-keys`
- `-lease-retry-attempts`, `-lease-retry-base-delay`

Terminal sessions enforce read limits, idle timeout, max duration, and write
deadlines. Browser terminal always requires the dev token. SSH terminal
requires public key authentication through the configured `authorized_keys`
file.

Execution lease recovery is request scoped and bounded. A node acknowledges an
accepted lease before gatewayd consumes terminal/process input or archive
chunks, and before gatewayd forwards streamed node output.
An authentication rejection before that boundary invalidates the old authority
and resolves a fresh lease from controld; gatewayd never retries the same
rejected token or retries after the node has accepted it. Allocation target and
lease refresh remain request-scoped and may retry only before the node accepts
authority or consumes client input.

## Local Smoke

```bash
make local-compose-up
make local-compose-server-base-smoke
```

The smoke creates a Run-backed Allocation and verifies allocation target
resolution plus terminal behavior. Tunnel behavior is covered separately by
`make local-compose-python-sdk-e2e`.

## Development Checks

```bash
go test ./...
go vet ./...
```

Run those from `gateway/gatewayd`. From the repo root, run:

```bash
make gatewayd-check-architecture
```
