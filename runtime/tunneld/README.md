# tunneld

`runtime/tunneld` owns Axern's internal raw TCP reverse tunnel data plane.

Components:

- `tunneld`: internal relay process that accepts gateway-forwarded client peers
  and node peers, validates them through `controld`, pairs peers by tunnel
  session, and forwards raw TCP stream frames.
- `node-tunneld`: node-local process shipped in `node-all-in-one`. It watches
  tunnel sessions from `controld`, resolves allocation network namespaces
  through the local `NodeOperator` Unix socket, binds `127.0.0.1:<remote_port>`
  inside the allocation netns, and forwards accepted TCP connections through
  `tunneld`.

The tunnel layer is intentionally runtime-neutral. It does not know about
Claude, OpenAI, Anthropic, HTTP headers, or WebSocket application semantics.
Scenario-specific adapters can run outside this layer and use the tunnel as a
plain TCP path.

## Build And Test

```bash
go test ./runtime/tunneld/...
go build ./runtime/tunneld/cmd/tunneld
go build ./runtime/tunneld/cmd/node-tunneld
go build ./runtime/tunneld/cmd/tunnel-agent
```

## Local Use

`controld` must advertise a tunnel relay registry with `-tunnel-relays`.
Entries use `id,client_target,node_target,weight,drain` and are separated by
semicolons. The `client_target` is the public gateway control edge address. The
`node_target` is the internal `tunneld` address. `controld` binds each tunnel
session to one non-draining relay, so gatewayd can route the foreground client
peer to the same `tunneld` process used by the node peer.
`node-tunneld` must be started with the same node id and node auth token used
by axnoded's control-plane reporter.

The user-facing foreground connector is:

```bash
axern tunnel open --allocation-id alloc-123 --local 127.0.0.1:8080
```

Claude Code can use a scenario adapter on top of the same raw TCP tunnel:

```bash
ANTHROPIC_BASE_URL=https://api.deepseek.com/anthropic \
ANTHROPIC_AUTH_TOKEN=... \
axern tunnel adapter claude-code --allocation-id alloc-123
```

The adapter starts a local Anthropic-compatible reverse proxy on
`127.0.0.1:0`, opens a tunnel to that proxy, and writes Claude Code config into
the allocation through gateway SSH for `--remote-user axern` by default:
`~/.claude/settings.json` and `~/.claude.json`. Existing files are backed up
with an `.axern.bak.<timestamp>` suffix before writing. Pass `--print-only` to
skip remote writes and print the snippets instead. The adapter keeps the local
token on the developer machine and overwrites inbound `Authorization` and
`x-api-key` headers before forwarding to the configured upstream.

Omit `--remote-port` to let controld allocate an available allocation-local
loopback port automatically, or pass `--remote-port <port>` when a fixed
allocation-local port is required.

External relay connections use gatewayd's public mTLS control edge by default.
Internal relay connections between gatewayd or `node-tunneld` and `tunneld` use
TLS with the relay server certificate.

The CLI creates a tunnel session in `controld`, waits for `node-tunneld` to
report the allocation-local bind ready by default, connects to gatewayd's
public tunnel relay edge, renews the session lease while the foreground
connector is alive, prints environment exports for common model clients, and
revokes the session on Ctrl-C. Renew failures that prove the session is
terminal stop the foreground connector; transient renew failures are retried
before the CLI exits.

Recent session history can be inspected without attaching to relay logs:

```bash
axern tunnel inspect <session-id>
axern tunnel events <session-id> --limit 50
axern tunnel doctor --session-id <session-id> --local 127.0.0.1:8080
```

The event stream is owned by `controld` and records durable lifecycle changes
such as create, renew, node status, relay peer connect/disconnect/pair,
resource limiting, revoke, and expiry. Events include a machine-readable reason
code plus free-form detail text, and are retained by `controld-retention`.
They are intended for debugging and operations, not for high-volume per-stream
traffic tracing.

## Operational Model

`tunneld` is an internal relay data-plane process, not the tunnel control-plane
owner and not the public ingress. It validates peers through `controld`, keeps
only in-memory peer pairing state, and can be restarted without changing tunnel
session ownership. Each session is bound to a relay id; gatewayd routes the
foreground client peer to that relay target, while `node-tunneld` reconnects to
the internal node target after relay loss.
Connected peers are also revalidated periodically, so revoked or expired
sessions converge by closing both relay peers even when their gRPC streams are
otherwise healthy.

Relay data-plane safeguards include protocol ping/pong, pair wait timeout,
maximum stream frame size, bounded peer send queues, drain mode, and active
session caps. Draining relays reject new peers but do not own control-plane
session state.

`node-all-in-one` runs `node-tunneld` under a local restart loop. A
`node-tunneld` crash or manual kill should not bring down `axnoded` or
`imagemgr`; the restarted daemon watches tunnel sessions again and rebinds
active allocation-local listeners.

The relay exposes OpenTelemetry metrics when `AXERN_OTEL_ENABLED=true`.
Current v1 metrics intentionally avoid high-cardinality session labels:

- `axern.tunneld_active_sessions`
- `axern.tunneld_active_peers`
- `axern.tunneld_peer_connect_total`
- `axern.tunneld_peer_disconnect_total`
- `axern.tunneld_frame_forward_total`
- `axern.tunneld_bytes_forward_total`

Use `tunneld -max-sessions <n>` to cap active relay session slots. The default
is `10000`; `0` disables the cap. A peer for a new session is rejected with
`ResourceExhausted` once the cap is reached. Replacing a peer for an existing
session is still allowed and closes the opposite peer so both sides reconnect
into a clean generation.

Use `tunneld -peer-revalidate-interval <duration>` to control connected-peer
revalidation. The default is `15s`; `0` disables periodic revalidation.

Session state is owned by `controld`:

- `pending`: session was created and is waiting for node bind/peer readiness.
- `running`: node has bound the allocation-local loopback port.
- `degraded`: data plane is temporarily unavailable but retryable.
- `revoked`: user/control-plane terminated the session.
- `expired`: TTL elapsed.
- `failed`: non-retryable setup failure such as invalid allocation netns or
  unrecoverable bind failure.
