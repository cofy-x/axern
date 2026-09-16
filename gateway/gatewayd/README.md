# gatewayd

`gatewayd` is Axern's unified external gateway for public control API traffic, Allocation-bound browser terminal and SSH-compatible sessions, process, file and archive operations, and foreground tunnel client peers.

It does not own placement, lifecycle, or durable state, and internal service traffic does not route through it by default. It resolves explicit Allocation targets through `controld`, then forwards traffic directly to the selected `axnoded` node using allocation-scoped access grants without creating a second control plane. Public `NodeSandbox` messages contain only the Allocation identity and operation input; gatewayd replaces any caller-supplied private access metadata and sends the controld-issued token only on the gateway-to-node gRPC hop.

Gateway-to-node traffic uses the gatewayd URI workload identity and verifies the exact Node ID returned by control-plane resolution. Configure `-workload-cluster`, `-workload-bundle` and `-tls-ca-cert`; credentials reload on every new connection. No shared Node certificate or alternate Node identity override exists.

External CLI and SDK control-plane gRPC traffic should terminate at `gatewayd`'s control edge listener, which is enabled by default. `controld` stays private inside the cluster; `gatewayd` verifies external client mTLS and forwards public control RPCs to the internal `controld` target with the dedicated `gatewayd` certificate. Caller-supplied internal identity metadata is discarded; gatewayd injects only the fingerprint of the leaf certificate it verified. Controld resolves that fingerprint to a durable Principal and applies platform or namespace role bindings on every RPC.

Tunnel foreground clients use the same public control edge. `gatewayd` registers `axern.tunnel.v1.TunnelRelay`, resolves the session-bound internal relay target through the private `GatewayControl` service, and forwards only client peers to `tunneld`. Peer authentication remains bound to the tunnel session token; gatewayd does not bypass it or treat the data stream as a public resource-management RPC. Node peers continue to connect directly to internal `tunneld` targets.

## Run

```bash
go run ./gateway/gatewayd \
  -http-address 127.0.0.1:25080 \
  -control-edge-address 127.0.0.1:25000 \
  -control-edge-tls-ca-cert .dev/certs/ca.crt \
  -control-edge-tls-cert .dev/certs/gatewayd.pem \
  -control-edge-tls-key .dev/certs/gatewayd.pem \
  -control-target 127.0.0.1:24000 \
  -tls-ca-cert .dev/certs/ca.crt \
  -workload-cluster axern.local \
  -workload-bundle .dev/certs/gatewayd.pem
```

Enable the optional SSH-compatible terminal listener with a persistent host key. Register client public keys as Principal Credentials using `axern admin credential add --ssh-public-key <file> --expires-at <RFC3339> <principal-id>`; existing Principal namespace roles authorize Allocation access:

```bash
go run ./gateway/gatewayd \
  -http-address 127.0.0.1:25080 \
  -control-edge-address 127.0.0.1:25000 \
  -control-edge-tls-ca-cert .dev/certs/ca.crt \
  -control-edge-tls-cert .dev/certs/gatewayd.pem \
  -control-edge-tls-key .dev/certs/gatewayd.pem \
  -ssh-enabled \
  -ssh-address 127.0.0.1:25022 \
  -ssh-host-key .dev/ssh/gateway_host_ed25519 \
  -control-target 127.0.0.1:24000 \
  -tls-ca-cert .dev/certs/ca.crt \
  -workload-cluster axern.local \
  -workload-bundle .dev/certs/gatewayd.pem
```

## Routes

- `GET /healthz` is served on the plaintext health listener.
- `wss://<control-edge>/terminal/allocation/{allocation_id}` requires a verified client X.509 Principal Credential on the shared TLS control listener. Tokens in URLs or headers do not authenticate a terminal.

Gateway data-plane routes are Allocation-bound and resolve their target through the control plane.

## SSH Terminal

When SSH is enabled, `ssh <allocation_id>@<gateway-host> -p <ssh-port>` opens an interactive `/bin/sh` session in the allocation through the same allocation-scoped `Process` stream used by the browser terminal. Use `ssh -t <allocation_id>@<gateway-host> -p <ssh-port> /bin/bash` to request a different interactive shell. Container users can be selected by sending `AXERN_EXEC_USER` in the SSH environment; the `axern ssh --user` command sets this for the common CLI path. The gateway terminates SSH; containers do not need to run `sshd`.

The SSH surface supports interactive `shell` sessions and non-interactive `exec` commands. Shell exec requests such as `/bin/bash` or `/bin/bash -l` are started directly; arbitrary exec requests run through `/bin/sh -lc` without a TTY unless the client requested one. It does not support SFTP, SCP, SSH agent forwarding, X11 forwarding, or SSH TCP forwarding.

## Observability

Gateway metrics, traces, and logs use the shared OpenTelemetry pipeline. Domain metrics cover allocation target resolution, upstream failures, lease retries, tunnel relay traffic, and active terminal or SSH sessions. Standard Go runtime metrics report heap, allocation, GC, goroutine, and scheduler behavior for long-running stability analysis. The deployment Prometheus scrapes the OTel Collector; gatewayd does not expose a separate production metrics endpoint.

Every request emits a structured access log with method, path, route type, status, duration, namespace, allocation id, node id, and error class. Logs and metrics never include plaintext access tokens, Authorization headers, or terminal stdin/stdout content.

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

`exit_code:-1` means the runtime finished the interaction but could not report a precise process exit status.

## Limits

Key flags/env:

- `-control-edge-address`
- `-control-edge-tls-ca-cert`, `-control-edge-tls-cert`, `-control-edge-tls-key`
- `-tunnel-relay-target`
- `-tunnel-relay-tls-ca-cert`, `-tunnel-relay-tls-server-name`
- `-workload-cluster`, `-workload-bundle`, `-tls-ca-cert`
- `-read-header-timeout`, `-read-timeout`, `-write-timeout`, `-idle-timeout`
- `-terminal-idle-timeout`, `-terminal-max-duration`, `-terminal-max-message-bytes`
- `-ssh-enabled`, `-ssh-address`, `-ssh-host-key`
- `-access-grant-retry-attempts`, `-access-grant-retry-base-delay`

Terminal sessions enforce read limits, idle timeout, max duration, and write deadlines. SSH public keys and WebSocket client certificates share Principal Credential revocation and namespace authorization. SSH certificates and multi-key credential inputs are not supported. Existing SSH and WebSocket process sessions revalidate authority every 15 seconds with a 5-second RPC deadline; rejection or inability to confirm authority closes access, not the Allocation. Grant expiry independently bounds the stream. The online revalidation bound is 20 seconds, excluding process scheduling pauses. Public gRPC operations are bounded by their issued grant deadline (normally five minutes); they do not yet share the terminal periodic revalidation path.

Allocation access-grant recovery is request scoped and bounded. A node acknowledges an accepted grant before gatewayd consumes terminal/process input or archive chunks, and before gatewayd forwards streamed node output. Before that boundary, gatewayd may retry the same committed grant and immutable Allocation binding while the node's control-plane watch catches up; it never advances the grant revision merely to repair projection visibility and never retries after the node has accepted authority or consumed client input. With the default five attempts, two-second node visibility wait and 500 ms linear backoff, an unavailable projection fails closed after approximately 15 seconds, excluding process scheduling pauses. A later client request obtains its own current grant through controld.

## Local Smoke

```bash
make local-compose-up
make local-compose-server-base-smoke
```

The smoke creates a Run-backed Allocation and verifies allocation target resolution plus terminal behavior. Tunnel behavior is covered separately by `make local-compose-python-sdk-e2e`.

## Development Checks

```bash
go test ./...
go vet ./...
```

Run those from `gateway/gatewayd`. From the repo root, run:

```bash
make gatewayd-check-architecture
```
