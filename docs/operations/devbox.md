# Axern Devbox Workflow

Axern's repo-local devbox is the default Linux workspace for source development
on the node runtime stack. It is started by `scripts/devbox/devbox.sh` through
root Make targets and does not depend on the private Runx devbox CLI.

The devbox image includes Go, Rust, Node.js, pnpm, Python, uv, Postgres,
Docker CLI, `runc`, `runsc`, SSH, and the filesystem/network/debug tools used
by runtime development.

## Start

Build the image once:

```bash
make devbox-image-build
```

Start the devbox container:

```bash
make devbox-up
```

`make devbox-up` starts `sshd`, mounts the repository at the same host path,
mounts the host Docker socket, and writes a managed `Host axern-devbox` block
to `~/.ssh/config`. It prints a VS Code Remote-SSH command such as:

```bash
code --remote ssh-remote+axern-devbox /path/to/axern
```

Enter without SSH when needed:

```bash
make devbox-shell
```

Check or stop the container:

```bash
make devbox-status
make devbox-down
```

## Standalone Stack

The recommended daily workflow is to run the Axern services directly inside the
devbox, without Docker Compose:

```bash
make devbox-stack-up
make devbox-stack-status
```

This starts repo-local Postgres plus:

- `controld`
- `storaged`
- `tunneld`
- `imagefsd`
- `imagemgr`
- `volumed`
- `axnoded`
- `node-tunneld`
- `gatewayd`

`gatewayd` starts with the browser dashboard enabled. Open:

```text
http://127.0.0.1:25080/dashboard?token=axern-local-dev
```

State and logs live under `.dev/stack`:

- Postgres data: `.dev/stack/postgres`
- Logs: `.dev/stack/logs`
- Pid files: `.dev/stack/pids`
- Dev-only helper binaries: `.dev/stack/bin`

For critical runtime log meanings, see [Runtime Logs](./runtime-logs.md).

Common operations from the host:

```bash
make devbox-stack-status
make devbox-stack-logs SERVICE=gatewayd
make devbox-stack-logs SERVICE=all
make devbox-stack-down
make devbox-stack-reset
```

When already attached to the devbox through VS Code Remote-SSH or
`make devbox-shell`, use the inner equivalents:

```bash
make dev-stack-status
make dev-stack-logs SERVICE=gatewayd
make dev-stack-logs SERVICE=all
make dev-stack-down
```

## Restart After Edits

For most source edits, restart only the affected service:

```bash
make devbox-stack-restart SERVICE=gatewayd
make devbox-stack-restart SERVICE=axnoded
make devbox-stack-restart SERVICE=imagemgr
make devbox-stack-restart SERVICE=volumed
make devbox-stack-restart SERVICE=imagefsd
make devbox-stack-restart SERVICE=tunneld
make devbox-stack-restart SERVICE=controld
make devbox-stack-restart SERVICE=storaged
```

Restart behavior is dependency-aware:

- `gatewayd`: restarts only `gatewayd`.
- `axnoded`: rebuilds the dev runtime runner, then restarts `axnoded` and
  `node-tunneld`.
- `volumed`: restarts `volumed`, `axnoded`, and `node-tunneld`.
- `imagemgr`: restarts `imagemgr`, `axnoded`, and `node-tunneld`.
- `imagefsd`: rebuilds `imagefsd`, then restarts `imagefsd`, `imagemgr`,
  `axnoded`, and `node-tunneld`.
- `tunneld`: rebuilds `tunnel-agent`, then restarts `tunneld` and
  `node-tunneld`.
- `controld`: runs migrations, then restarts `controld` plus services that
  depend on the control plane.
- `storaged`: restarts `storaged`, `controld`, `axnoded`, `node-tunneld`, and
  `gatewayd`.
- `postgres`: restarts the full stack while keeping Postgres data.

Inside the devbox, use `make dev-stack-restart SERVICE=<name>`.

## Single-Service Debugging

For focused debugging, prepare the `.dev` workspace and launch only the service
you want:

```bash
make node-dev-prepare
make postgres-dev-up
make storaged-dev-run
make controld-dev-run
make gatewayd-dev-run
make imagefsd-dev-serve-chunk
make imagemgr-dev-run
make volumed-dev-run
make axnoded-dev-run
```

`node-dev-prepare` also builds `.dev/stack/bin/axnoded-runtime-runner` and
writes that path into `.dev/axnoded/config.toml`, so `axnoded-dev-run` and VS
Code's `Axnoded: Debug daemon` use the same one-shot OCI runtime helper as the
standalone stack.

Stop the standalone stack before switching to single-service debugging:

```bash
make dev-stack-down
```

The full stack and the debug launch targets use the same local ports and
sockets, so running both at once causes port or socket conflicts.

Standalone Delve DAP helpers are also available for terminal-driven debugging:

```bash
make axnoded-debug-server
make imagemgr-debug-server
```

These start external Delve DAP servers on:

- `axnoded`: `127.0.0.1:43001`
- `imagemgr`: `127.0.0.1:43002`

The checked-in VS Code configuration uses these targets:

- `.vscode/tasks.json` exposes stack up/status/restart and debug preparation.
- `.vscode/launch.json` has `Controld: Debug daemon`,
  `Storaged: Debug daemon`, `Gatewayd: Debug daemon`, `Axnoded: Debug daemon`,
  `Imagemgr: Debug daemon`, `Volumed: Debug daemon`, and
  `Imagefsd: Debug chunk server`.

Use these from a VS Code Remote-SSH window attached to `axern-devbox`, so tasks
run inside the Linux devbox. `axnoded`, `imagemgr`, and `volumed` use the Go
extension's `asRoot` launch mode, so VS Code starts Delve through passwordless
`sudo` instead of connecting to a separately launched DAP port.

Accept the workspace extension recommendations in `.vscode/extensions.json`
inside the Remote-SSH window. The Go and C/C++ debug configuration schemas are
contributed by `golang.go` and `ms-vscode.cpptools`; without those extensions
enabled on the remote side, VS Code reports the `go` and `cppdbg` debug types as
unknown even though the repository configuration is valid for those extensions.

A practical manual debug startup order is:

1. `Storaged: Debug daemon`
2. `Controld: Debug daemon`
3. `Gatewayd: Debug daemon`
4. `Imagefsd: Debug chunk server`
5. `Imagemgr: Debug daemon`
6. `Volumed: Debug daemon`
7. `Axnoded: Debug daemon`

`Axnoded: Debug daemon` registers `axern-dev-node` with the standalone
`controld` at `127.0.0.1:24000` by default. Product CLI commands use
`gatewayd`'s control edge at `127.0.0.1:25000`, so workloads can be placed after
the seven debug services are running.

Catalog-backed workloads need their runtime images imported into standalone
`imagemgr`. This mirrors the compose/kind image load flow:

```bash
make dev-runtime-images-load
```

The default imports `python311`. Pass more images when needed:

```bash
make dev-runtime-images-load DEV_RUNTIME_IMAGES='python311 server-base coding-base desktop-base claude-code-bundle codex-bundle'
```

From the host, use:

```bash
make devbox-runtime-images-load
```

## Gateway Smoke With A Python Service

After the standalone stack is running and the `python311` runtime image is
loaded, you can start a tiny Python HTTP service and reach it through
`gatewayd`. Run this from a devbox shell or VS Code Remote-SSH terminal:

```bash
make axern-dev-build
make dev-runtime-images-load

SERVICE_ID="$(
  axern svc create \
    --template-id python311 \
    --runtime-class runc \
    --replicas 1 \
    --readiness-http-port 8080 \
    --readiness-http-path / \
    --argv=python \
    --argv=-m \
    --argv=http.server \
    --argv=8080 \
    --argv=--bind \
    --argv=0.0.0.0 \
    -o json | jq -r '.service.id'
)"

for _ in $(seq 1 60); do
  if [ "$(axern svc get "${SERVICE_ID}" -o json | jq -r '.service.ready_replicas')" = "1" ]; then
    break
  fi
  sleep 2
done

axern svc replicas "${SERVICE_ID}"
curl -fsS "http://127.0.0.1:25080/svc/default/${SERVICE_ID}/8080/" | head
```

The gateway URL format is
`/svc/{namespace}/{service_id}/{port}/...`; the standalone `gatewayd` listens
on `127.0.0.1:25080`.

Clean up the service when finished:

```bash
axern svc delete --purge --wait "${SERVICE_ID}"
```

## Standalone CLI Helpers

The devbox exports standalone defaults for both `docker exec` and VS Code
Remote-SSH shells:

```bash
AXERN_ENDPOINT=127.0.0.1:25000
AXERN_PROXY_MODE=direct
AXERN_TLS_CA_CERT=$AXERN_DEV_WORKSPACE/.dev/certs/ca.crt
AXERN_TLS_CERT=$AXERN_DEV_WORKSPACE/.dev/certs/client.crt
AXERN_TLS_KEY=$AXERN_DEV_WORKSPACE/.dev/certs/client.key
AXNODED_SOCKET=$AXERN_DEV_WORKSPACE/.dev/run/axnoded.sock
IMAGEMGR_SOCKET=$AXERN_DEV_WORKSPACE/.dev/run/imagemgr.sock
```

So direct CLI development commands work without extra flags:

```bash
go -C apps/cli run . catalog list
go -C runtime/axnoded run ./axctl node check
```

When the standalone services are running, repo-local Make wrappers remain
available as shorter stable aliases:

```bash
make axern-dev ARGS='svc list'
make axctl-dev ARGS='node check'
```

`axern-dev` runs `apps/cli`; `axctl-dev` runs `runtime/axnoded/axctl`.

From the host, the matching devbox wrappers execute the same commands inside
the running container:

```bash
make devbox-axern ARGS='svc list'
make devbox-axctl ARGS='node check'
```

For repeated manual use, build repo-local binaries:

```bash
make axern-dev-build
make axctl-dev-build
```

These binaries are written to the repo-local `bin/` directory, which the devbox
adds to `PATH` as `$AXERN_DEV_WORKSPACE/bin`.

## Image Build Network Defaults

Image builds use upstream Ubuntu and language package sources by default, with
no automatically detected host proxy:

```bash
make devbox-image-build
```

Use an explicit regional mirror or proxy when the local network needs one:

```bash
DEVBOX_APT_MIRROR_SOURCE=aliyun \
DEVBOX_BUILD_PROXY=auto \
GOPROXY=https://goproxy.cn,direct \
NPM_CONFIG_REGISTRY=https://registry.npmmirror.com \
make devbox-image-build
```

`DEVBOX_BUILD_PROXY=auto` uses `http://host.docker.internal:7890` only when
`127.0.0.1:7890` is reachable. A proxy URL can also be passed directly.
`GOPROXY` and `NPM_CONFIG_REGISTRY` apply to both the image build and the
running devbox.

The devbox wrapper does not set runtime proxy variables inside the container.
This keeps VS Code Remote-SSH server downloads, extension installation, and
normal shell use independent of host proxy latency. If registry or package
downloads inside the running devbox need a proxy, set it in that shell:

```bash
export HTTP_PROXY=http://host.docker.internal:7890
export HTTPS_PROXY=http://host.docker.internal:7890
export NO_PROXY=localhost,127.0.0.1,::1,host.docker.internal,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,.svc,.cluster.local
```

The standalone stack preserves those proxy variables when it starts root-owned
runtime daemons through `sudo`.

Select another supported mirror explicitly when needed:

```bash
DEVBOX_APT_MIRROR_SOURCE=ustc make devbox-image-build
```

## SSH Customization

Override the host SSH port:

```bash
DEVBOX_SSH_PORT=23022 make devbox-up
```

Override the SSH alias:

```bash
DEVBOX_SSH_CONFIG_HOST=axern-devbox-arm64 make devbox-up
```

The generated SSH config block is managed by the devbox wrapper and does not
rewrite unrelated entries in `~/.ssh/config`.

## Docker Verification

The standalone stack does not require Docker Compose, but the devbox still
mounts the host Docker socket for verification flows that intentionally test
image and container behavior:

```bash
make axnoded-verify-docker
```
