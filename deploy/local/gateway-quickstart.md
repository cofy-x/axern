# Gateway Quickstart

This page keeps copyable local commands for checking gatewayd in compose and
kind.

It covers two gateway paths:

- service HTTP forwarding through `/svc/{namespace}/{service_id}/{port}/...`
- interactive allocation terminals through the browser dashboard or SSH

## Local Endpoints

| Environment | HTTP gateway | Dashboard | SSH |
| --- | --- | --- | --- |
| compose | `http://127.0.0.1:25080` | `http://127.0.0.1:25080/dashboard?token=axern-local-dev` | `127.0.0.1:25022` |
| kind | `http://127.0.0.1:25082` | `http://127.0.0.1:25082/dashboard?token=axern-local-dev` | `127.0.0.1:25023` |

`axern ssh` reads the SSH target and generated client key from the current
axern context. Use `axern ctx list` before testing if both compose and kind
are running.

## Server Base Default Entrypoint

After starting compose or kind and selecting its `axern` context, create a
`server-base` service without passing `--argv`. Axern will use the image default
entrypoint, which starts `supervisord` and the built-in nginx smoke endpoint.

```bash
gateway_url="http://127.0.0.1:25080" # compose
# gateway_url="http://127.0.0.1:25082" # kind

namespace="default"

svc_id="$(axern svc create \
  -o json \
  --template-id server-base \
  --replicas 1 \
  --readiness-http-port 80 \
  --readiness-http-path / \
  --readiness-period 1s \
  --readiness-timeout 1s \
  | jq -r '.service.id')"

until axern svc get -o json "$svc_id" |
  jq -e '.service.status == "ready" and .service.ready_replicas == 1' >/dev/null; do
  sleep 2
done

curl -fsS "${gateway_url}/svc/${namespace}/${svc_id}/80/"
```

Expected response:

```text
axern-server-base-ok
```

Clean up:

```bash
axern svc delete -o json "$svc_id"
axern svc purge -o json "$svc_id"
```

## Compose

Start or refresh compose:

```bash
make local-images-build
make local-compose-up
axern ctx use compose
axern ctx list
```

Compose and kind start the local OpenTelemetry stack by default. To disable it
for a leaner run:

```bash
OTEL=0 make local-compose-up
OTEL=0 make kind-up
```

For compose, use Grafana LGTM at `http://127.0.0.1:13000`. For kind, use
`http://127.0.0.1:13001`. LGTM includes Tempo, Loki, Prometheus, and Grafana.
Filter by `service.name`, `axern.service_id`, `axern.allocation_id`, or
`axern.node_id`.

### Service HTTP

Create a long-running Python HTTP service:

```bash
http_namespace="default"

http_script='from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = f"gateway-python-ok path={self.path}\n".encode()
        self.send_response(200)
        self.send_header("content-type", "text/plain")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *args):
        return

ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()'

http_svc_id="$(axern svc create \
  -o json \
  --template-id python311 \
  --replicas 1 \
  --argv python \
  --argv -u \
  --argv -c \
  --argv "$http_script" \
  --readiness-http-port 8080 \
  --readiness-http-path /ready \
  --readiness-period 1s \
  --readiness-timeout 1s \
  | jq -r '.service.id')"

until axern svc get -o json "$http_svc_id" |
  jq -e '.service.status == "ready" and .service.ready_replicas == 1' >/dev/null; do
  sleep 2
done

curl -fsS "http://127.0.0.1:25080/svc/${http_namespace}/${http_svc_id}/8080/smoke"
```

Expected response:

```text
gateway-python-ok path=/smoke
```

Clean up:

```bash
axern svc delete -o json "$http_svc_id"
axern svc purge -o json "$http_svc_id"
```

### Terminal And SSH

Create a one-hour service:

```bash
svc_id="$(axern svc create \
  -o json \
  --template-id python311 \
  --replicas 1 \
  --argv python \
  --argv -u \
  --argv -c \
  --argv 'import time; time.sleep(3600)' \
  | jq -r '.service.id')"
```

Wait for a ready allocation and capture its id:

```bash
until allocation_id="$(axern svc replicas --view current -o json "$svc_id" |
  jq -r '.replicas[] | select(.ready == true and (.ended // false) == false and (.outdated // false) == false) | .id' |
  head -n1)" && [ -n "$allocation_id" ]; do
  sleep 2
done

echo "allocation_id=$allocation_id"
```

Open the browser terminal dashboard:

```text
http://127.0.0.1:25080/dashboard?token=axern-local-dev
```

Paste `svc_id` into the target field and connect. If the service has one
current ready replica, the dashboard connects automatically; if it has multiple
current ready replicas, pick one from the displayed list. You can also paste
`allocation_id` directly. The compose gateway image includes the xterm assets
when built through `make local-images-build` or `make local-compose-up`.

SSH into the service. If the service has one current ready replica, the CLI
selects it automatically:

```bash
axern ssh "$svc_id"
```

Request a specific interactive shell:

```bash
axern ssh --shell /bin/bash "$svc_id"
```

If a service has multiple current ready replicas, the CLI prompts you to pick
one. For scripts, pass the allocation explicitly:

```bash
axern ssh --allocation-id "$allocation_id" "$svc_id"
```

Raw OpenSSH form:

```bash
ssh -t -i deploy/local/state/compose/ssh/gateway_client_ed25519 \
  -p 25022 \
  "$allocation_id@127.0.0.1" \
  /bin/bash
```

Clean up:

```bash
axern svc delete -o json "$svc_id"
axern svc purge -o json "$svc_id"
```

The automated compose check is:

```bash
make local-compose-gateway-ssh-e2e
```

## Kind

Start or refresh the repo-managed kind environment:

```bash
make local-images-build
make kind-up
axern ctx use kind
axern ctx list
```

### Service HTTP

Create a long-running Python HTTP service:

```bash
http_namespace="default"

http_script='from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        body = f"gateway-python-ok path={self.path}\n".encode()
        self.send_response(200)
        self.send_header("content-type", "text/plain")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt, *args):
        return

ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()'

http_svc_id="$(axern svc create \
  -o json \
  --template-id python311 \
  --replicas 1 \
  --argv python \
  --argv -u \
  --argv -c \
  --argv "$http_script" \
  --readiness-http-port 8080 \
  --readiness-http-path /ready \
  --readiness-period 1s \
  --readiness-timeout 1s \
  | jq -r '.service.id')"

until axern svc get -o json "$http_svc_id" |
  jq -e '.service.status == "ready" and .service.ready_replicas == 1' >/dev/null; do
  sleep 2
done

curl -fsS "http://127.0.0.1:25082/svc/${http_namespace}/${http_svc_id}/8080/smoke"
```

Expected response:

```text
gateway-python-ok path=/smoke
```

Clean up:

```bash
axern svc delete -o json "$http_svc_id"
axern svc purge -o json "$http_svc_id"
```

### Terminal And SSH

Create a one-hour service:

```bash
svc_id="$(axern svc create \
  -o json \
  --template-id python311 \
  --replicas 1 \
  --argv python \
  --argv -u \
  --argv -c \
  --argv 'import time; time.sleep(3600)' \
  | jq -r '.service.id')"
```

Wait for a ready allocation and capture its id:

```bash
until allocation_id="$(axern svc replicas --view current -o json "$svc_id" |
  jq -r '.replicas[] | select(.ready == true and (.ended // false) == false and (.outdated // false) == false) | .id' |
  head -n1)" && [ -n "$allocation_id" ]; do
  sleep 2
done

echo "allocation_id=$allocation_id"
```

Open the browser terminal dashboard through the repo-managed kind NodePort
gateway:

```text
http://127.0.0.1:25082/dashboard?token=axern-local-dev
```

Paste `svc_id` into the target field and connect. If the service has one
current ready replica, the dashboard connects automatically; if it has multiple
current ready replicas, pick one from the displayed list. You can also paste
`allocation_id` directly.

SSH into the service. If the service has one current ready replica, the CLI
selects it automatically:

```bash
axern ssh "$svc_id"
```

Request a specific interactive shell:

```bash
axern ssh --shell /bin/bash "$svc_id"
```

If a service has multiple current ready replicas, the CLI prompts you to pick
one. For scripts, pass the allocation explicitly:

```bash
axern ssh --allocation-id "$allocation_id" "$svc_id"
```

Raw OpenSSH form for kind:

```bash
ssh -t -i deploy/local/state/kind/ssh/gateway_client_ed25519 \
  -p 25023 \
  "$allocation_id@127.0.0.1" \
  /bin/bash
```

Clean up:

```bash
axern svc delete -o json "$svc_id"
axern svc purge -o json "$svc_id"
```

The automated kind check is:

```bash
make kind-gateway-smoke
```
