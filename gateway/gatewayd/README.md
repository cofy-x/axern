# gatewayd

`gatewayd` is Axern's external entry point for public control API traffic,
control-plane-managed services, browser terminal sessions, and optional
SSH-compatible terminal sessions. It also owns the public tunnel relay entry
for foreground client peers.

It does not own placement, lifecycle, or durable state. It resolves service
routes through `controld`, then forwards traffic directly to the selected
`axnoded` node using allocation-scoped leases.

External CLI and SDK control-plane gRPC traffic should terminate at
`gatewayd`'s control edge listener, which is enabled by default. `controld`
stays private inside the cluster; `gatewayd` verifies external client mTLS and
forwards public control RPCs to the internal `controld` target with the
dedicated `gatewayd` certificate. That identity is also the only certificate
authorized to call private `ArtifactAccess`; the generic platform client
certificate cannot resolve download tickets.

Tunnel foreground clients use the same public control edge. `gatewayd`
registers `axern.tunnel.v1.TunnelRelay`, resolves the session-bound internal
relay target through `controld`, and forwards only client peers to `tunneld`.
Node peers continue to connect directly to internal `tunneld` targets.

Rollout artifact downloads also terminate at the control edge. The client
first obtains a short-lived artifact-bound ticket through the public rollout
API, then calls `ArtifactData.Download`. Gatewayd resolves the ticket through
controld's private mTLS `ArtifactAccess` API and streams the internal
presigned object-store response with offset validation and backpressure. It
does not hold S3 credentials, expose the internal URL, or log tickets.

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

Artifact data-plane limits are configured with
`-artifact-max-concurrent`, `-artifact-chunk-bytes`,
`-artifact-upstream-timeout`, and `-artifact-max-bytes`.

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
- `/svc/{namespace}/{service_id}/{port}/...` proxies HTTP traffic to a READY service replica
- `POST /function/invoke` is the internal Function worker dispatch path used by
  `controld`; it resolves the worker service route, rewrites to `/invoke`, and
  forwards through the same node lease proxy as `/svc`
- `/terminal/allocation/{allocation_id}` opens a WebSocket terminal and requires the dev token
- `/dashboard` serves the optional lightweight terminal dashboard when enabled and requires the dev token

V1 uses path routing only. `{service_id}` is the existing Axern service id, and
`{port}` is either `PortSpec.name` or a container port number.
`/function/invoke` expects `X-Axern-Namespace`,
`X-Axern-Worker-Service-Id`, and optional `X-Axern-Worker-Port` headers; when a
dev token is configured it requires `Authorization: Bearer <token>`.

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

## Dashboard

Enable the optional browser dashboard by first downloading untracked xterm
assets and then starting `gatewayd` with dashboard enabled:

```bash
make gateway-dashboard-assets

go run ./gateway/gatewayd \
  -http-address 127.0.0.1:25080 \
  -dashboard-enabled \
  -dashboard-vendor-dir gateway/gatewayd/internal/api/http/dashboard/vendor \
  -control-target 127.0.0.1:24000 \
  -tls-ca-cert .dev/certs/ca.crt \
  -tls-cert .dev/certs/gatewayd.crt \
  -tls-key .dev/certs/gatewayd.key \
  -dev-token axern-local-dev
```

Open `http://127.0.0.1:25080/dashboard?token=axern-local-dev`, enter an
allocation id or service id, and connect. Service ids are resolved in the
dashboard: one current ready replica connects automatically, while multiple
current ready replicas are shown as a small allocation picker. The terminal
connection still uses the existing `/terminal/allocation/{allocation_id}`
WebSocket path.

## Observability

Gateway metrics, traces, and logs use the shared OpenTelemetry pipeline. Domain
metrics cover service proxy stages, route cache and resolve events, upstream
failures, lease retries, active HTTP requests, and active terminal sessions.
Artifact metrics cover active downloads, bytes, duration, ticket resolution,
resume, and bounded rejection/error classes without artifact IDs or tickets.
The route cache exports bounded route, endpoint, quarantine, and in-flight
entry gauges. Standard Go runtime metrics report heap, allocation, GC,
goroutine, and scheduler behavior for long-running stability analysis.
The deployment Prometheus scrapes the OTel Collector; gatewayd does not expose
a separate production metrics endpoint.

Every request emits a structured access log with method, path, route type,
status, duration, namespace, service id, port, allocation id, node id, and
error class. Logs and metrics never include plaintext lease tokens,
Authorization headers, or terminal stdin/stdout content.
Artifact tickets, internal object-store URLs, query strings, and upstream
Authorization headers are also excluded from logs, metrics, and traces.

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
- `-service-upstream-timeout`, `-service-max-request-body-bytes`
- `-route-cache-ttl`, `-route-cache-max-entries`
- `-terminal-idle-timeout`, `-terminal-max-duration`, `-terminal-max-message-bytes`
- `-ssh-enabled`, `-ssh-address`, `-ssh-host-key`, `-ssh-authorized-keys`
- `-dashboard-enabled`, `-dashboard-vendor-dir`
- `-lease-retry-attempts`, `-lease-retry-base-delay`
- `-artifact-max-concurrent`, `-artifact-chunk-bytes`
- `-artifact-upstream-timeout`, `-artifact-max-bytes`

Service proxy request bodies are capped, while streaming responses remain
allowed. Terminal sessions enforce read limits, idle timeout, max duration, and
write deadlines. Browser terminal always requires the dev token. SSH terminal
requires public key authentication through the configured `authorized_keys`
file. Service auth remains controlled by `-require-http-auth`.

Execution lease recovery is request scoped and bounded. A node acknowledges an
accepted lease before gatewayd consumes terminal/process input, an HTTP request
body, or archive chunks, and before gatewayd forwards streamed node output.
An authentication rejection before that boundary invalidates the old authority
and resolves a fresh lease from controld; gatewayd never retries the same
rejected token or retries after the node has accepted it. Service
lease refresh and endpoint failover share `-service-endpoint-retry-attempts` as
one total attempt budget. Unsafe HTTP requests are retried only for a confirmed
pre-upstream lease rejection, while the original request body is still
unconsumed. Endpoint failures continue to retry only replayable idempotent
requests.

## Local Smoke

```bash
make local-compose-up
make local-compose-gateway-smoke
```

The smoke creates a temporary Python HTTP service, checks `/svc/...`, verifies a
terminal WebSocket `echo`, then deletes and purges the service.

## Development Checks

```bash
go test ./...
go vet ./...
```

Run those from `gateway/gatewayd`. From the repo root, run:

```bash
make gatewayd-check-architecture
```
