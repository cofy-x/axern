# Axern Tunnel

`axern svc tunnel` opens a foreground reverse TCP tunnel from a ready service
replica to a local TCP service on the machine running `axern`.

Use it when code running inside an Axern service needs to call something local
to your workstation, such as a development API server, a mock server, or a local
credential-holding proxy.

This is the opposite of a normal port-forward. Your local machine does not use
the tunnel to call the remote service. The remote allocation calls a localhost
port that Axern binds inside the allocation, and that traffic is forwarded back
to your local target.

## Start A Local Target

Start the local TCP service first. For example:

```bash
python3 -m http.server 8080 --bind 127.0.0.1
```

The local target is the address the CLI connector will dial:

```text
127.0.0.1:8080
```

## Open A Service Tunnel

Create or choose a ready service:

```bash
axern svc create --template-id python311 --replicas 1
axern svc get <service_id>
axern svc replicas <service_id>
```

Open the tunnel:

```bash
axern svc tunnel <service_id> --to 127.0.0.1:8080
```

The command selects a current ready service replica, creates a tunnel session,
waits for the allocation-local bind to become ready, and starts the foreground
connector. Keep the command running while the remote workload needs the local
target.

The output includes the selected allocation, tunnel session, local target, and
remote bind address:

```text
Service: svc-...
Selected allocation: alloc-...
Tunnel session: tun-...
Local target: 127.0.0.1:8080
Remote bind: 127.0.0.1:42377
Press Ctrl-C to revoke the tunnel.
```

## Use It From The Remote Service

Inside the selected allocation, connect to the printed remote bind address:

```bash
curl http://127.0.0.1:42377/
```

That request flows through `node-tunneld`, the internal relay, gatewayd's
public tunnel relay edge, and the local CLI connector, then reaches your local
`127.0.0.1:8080` target.

If the service has multiple ready replicas, `axern svc tunnel` chooses a stable
ready allocation and prints the selected allocation and node. To target a
specific replica, pass:

```bash
axern svc tunnel <service_id> \
  --allocation-id <allocation_id> \
  --to 127.0.0.1:8080
```

Or select the ready replica currently running on a specific node:

```bash
axern svc tunnel <service_id> \
  --node-id <node_id> \
  --to 127.0.0.1:8080
```

## Allocation-Level Debugging

Use `axern tunnel open` when you already know the allocation id or need to test
the allocation-level primitive directly:

```bash
axern tunnel open \
  --allocation-id <allocation_id> \
  --local 127.0.0.1:8080
```

`svc tunnel` is the recommended product entrypoint for normal service workflows.
`tunnel open` is the lower-level debugging entrypoint for allocation-scoped
reverse TCP tunnel behavior.

## Diagnose A Tunnel

List and inspect active sessions:

```bash
axern tunnel list --allocation-id <allocation_id>
axern tunnel inspect <session_id>
axern tunnel events <session_id>
```

Run doctor when the remote allocation cannot reach the local target:

```bash
axern tunnel doctor \
  --session-id <session_id> \
  --local 127.0.0.1:8080
```

You can also diagnose from the service view. Doctor checks current ready
replicas and active tunnel sessions for the service:

```bash
axern tunnel doctor \
  --service-id <service_id> \
  --local 127.0.0.1:8080
```

Doctor checks control-plane state, gateway relay reachability, recent peer
events, client and node peer summaries, and the optional local upstream TCP
probe. Its JSON output intentionally excludes tunnel tokens.

Tunnel relay connections use the gateway control edge mTLS path by default.
Repo-managed compose/kind contexts configure the CA and client certificate
automatically, so development and production use the same public entry model.

## Cleanup

Stop the foreground tunnel with Ctrl-C. The CLI revokes the tunnel session on
exit.

If you need to close a session explicitly:

```bash
axern tunnel revoke <session_id> --reason manual-cleanup
```

Delete the service when finished:

```bash
axern svc delete --purge --wait <service_id>
```
