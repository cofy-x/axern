# Local Deployment

This directory contains the repo-supported local truth environments:

- `compose/`: Docker Compose deployment
- `kind/`: repo-managed 3-node kind deployment
- `k8s/`: manifests shared by the kind flow
- `otel/`: optional local OpenTelemetry/LGTM config
- `state/`: generated local PKI, CLI env files, SSH keys, and runtime state

For gateway dashboard, service, terminal, and SSH examples, see
[Gateway Quickstart](gateway-quickstart.md).

## Start

```bash
make local-compose-up
make kind-up
```

Both flows build local deploy images before starting services. They also write
CLI env files and refresh the local `axern` context config:

- `deploy/local/state/compose/axern.env`
- `deploy/local/state/kind/axern.env`

`make kind-up` also starts or reuses a Docker-backed repo-managed local
registry at `127.0.0.1:5001`. New repo-managed kind clusters mirror
`localhost:5001` to that registry through the Docker `kind` network. If an
existing kind cluster was created before this mirror existed or before the
registry container was named `axern-registry`, run `make kind-reset` to
recreate it.

Registry defaults:

- `LOCAL_REGISTRY_NAME=axern-registry`
- `LOCAL_REGISTRY_PORT=5001`
- `LOCAL_REGISTRY_IMAGE=registry:2`
- `LOCAL_REGISTRY_HOST=localhost:5001`
- `LOCAL_REGISTRY_CLUSTER_HOST=host.docker.internal:5001`

Nydus smoke defaults are repo-managed and self-contained:

- `make local-compose-nydus-smoke` and `make kind-axern-nydus-smoke` build a
  local Nydus image into the registry when it is missing.
- The default source image is `axern/python311-runtime:dev`.
- Set `NYDUS_TEST_IMAGE` only when intentionally validating a custom or
  production-built Nydus image.

Useful status commands:

```bash
make local-compose-status
make kind-status
```

For runtime stack failures, use the compose/kind commands in
[Local Troubleshooting](troubleshooting.md).

For kind kube access:

```bash
eval "$(make kube-env-kind)"
kubectl get nodes
```

## Daily Verification

Use the refresh path for normal development after both environments already
exist:

```bash
make local-refresh-verify
```

This runs:

```bash
make local-compose-refresh-verify
make kind-refresh-verify
```

Refresh verification keeps the existing compose project or kind cluster,
rebuilds local images, resets the environment database/state, reruns
migrations, redeploys core services, reimports catalog runtime images, and runs
the core smoke suites.

Use targeted refreshes when you only need one environment:

```bash
make local-compose-refresh-verify
make kind-refresh-verify
```

Use the clean-slate truth check before larger handoffs or when environment
state itself is suspect:

```bash
make local-truth-verify
```

`local-truth-verify` purges and recreates compose and kind, then runs the full
local smoke suite.

For storage and volume-only changes, use the narrower cross-environment entry:

```bash
make local-storage-verify
```

It runs the compose and kind service-volume truth-path smokes against the
current environments without resetting them. To include the same local storage
pass in the full serial repository verifier, use:

```bash
bash ./scripts/verify-all.sh --include-local-storage
```

## Targeted Smoke

```bash
make local-compose-smoke
make local-compose-gateway-smoke
make local-compose-service-volume-smoke
make local-compose-run-smoke
make local-compose-invoke-smoke
make local-compose-function-smoke
make local-compose-server-base-smoke
make local-compose-quota-smoke
make local-compose-computer-use-e2e

make kind-smoke
make kind-gateway-smoke
make kind-service-volume-smoke
make kind-run-smoke
make kind-invoke-smoke
make kind-server-base-smoke
make kind-quota-smoke
```

Resource admission, quota, request/limit, or node inventory changes should at
least run:

```bash
make local-compose-smoke
make local-compose-run-smoke
make local-compose-quota-smoke
make kind-smoke
make kind-run-smoke
make kind-quota-smoke
```

`*-service-volume-smoke` is intentionally heavier than the basic service and
run checks: it verifies volume publish, rollout replacement, node-runtime
restart recovery, and storage failure injection. The scripts print
`*_service_volume_smoke_phase=...` markers so slow or failing runs show the
active phase.

Tunnel-specific changes should also run:

```bash
make local-compose-tunnel-e2e
make kind-tunnel-e2e
make kind-tunnel-relay-e2e
make kind-tunnel-multirelay-e2e
make tunnel-benchmark-compose
```

`kind-tunnel-relay-e2e` is the fast control-plane registry/drain check.
`kind-tunnel-multirelay-e2e` creates two physical kind relay deployments and
verifies session-bound relay behavior across drain and relay loss. The
benchmark target records a compose baseline only; it is not a hard performance
gate.

Image-backed service checks are intentionally separate because registry-first
paths and optional external image overrides can depend on registry or proxy
reachability:

```bash
make local-compose-image-service-smoke
make local-compose-registry-image-smoke
make local-compose-image-mount-smoke
make local-compose-claude-code-image-mount-smoke
make local-compose-codex-image-mount-smoke
make local-compose-nydus-smoke
make kind-image-service-smoke
make kind-axern-registry-image-smoke
make kind-axern-nydus-smoke
```

- `local-compose-registry-image-smoke` pushes a local runtime image into the
  repo-managed local registry and starts an Axern run from the registry ref
  through the compose node runtime path.
- `local-compose-image-mount-smoke` pushes a task image and a tiny reusable
  image bundle into the repo-managed local registry, starts an Axern run with
  `--image-mount`, and verifies the mounted bundle is executable and read-only.
- `local-compose-claude-code-image-mount-smoke` builds a Claude Code
  read-only bundle, mounts it into a coding-base task sandbox, runs
  `claude --version`, and verifies the mount is read-only without using
  provider credentials.
- `local-compose-codex-image-mount-smoke` does the same for the Codex CLI
  bundle and validates the Node/npm launcher shape inside the task sandbox.
- `kind-axern-registry-image-smoke` pushes a local runtime image into the
  repo-managed local registry and starts an Axern run from the registry ref.
- `local-compose-nydus-smoke` and `kind-axern-nydus-smoke` validate Axern's own
  Nydus path with a repo-built local Nydus image by default:
  `registry source image -> nydus builder -> registry Nydus image -> imagemgr
  -> imagefsd -> axnoded -> sandbox`.
- The Nydus smoke does not install or test the Kubernetes `nydus-snapshotter`
  path.

The local node-all-in-one image starts `imagefsd serve-chunk` alongside
`imagemgr`. Its Unix socket lives at
`/var/lib/imagemgr/chunk_db/chunkserver.sock`, so `imagemgr /inventory` can
report chunkdb/locality state without degrading `imagefsd` readiness.

Refresh verification can opt into those broader checks:

```bash
COMPOSE_REFRESH_TUNNEL_E2E=1 make local-compose-refresh-verify
COMPOSE_REFRESH_TUNNEL_BENCHMARK=1 make local-compose-refresh-verify
COMPOSE_REFRESH_IMAGE_SERVICE_SMOKE=1 make local-compose-refresh-verify
LOCAL_COMPOSE_REGISTRY_IMAGE_SMOKE=1 make local-compose-refresh-verify
LOCAL_COMPOSE_AXERN_NYDUS_SMOKE=1 make local-compose-refresh-verify
KIND_REFRESH_TUNNEL_E2E=1 make kind-refresh-verify
KIND_REFRESH_TUNNEL_RELAY_E2E=1 make kind-refresh-verify
KIND_REFRESH_TUNNEL_MULTIRELAY_E2E=1 make kind-refresh-verify
KIND_REFRESH_IMAGE_SERVICE_SMOKE=1 make kind-refresh-verify
KIND_REFRESH_REGISTRY_IMAGE_SMOKE=1 make kind-refresh-verify
KIND_REFRESH_AXERN_NYDUS_SMOKE=1 make kind-refresh-verify
```

`make local-truth-verify` is the clean full pass and runs the tunnel e2e,
benchmark, relay registry, and multi-relay checks by default after rebuilding
local images and resetting compose/kind.

## Local Images

Build local images without starting an environment:

```bash
make local-images-build
```

To use a host-built image that is not available from a registry, import it into
the node image cache first:

```bash
docker build -t myapp:dev .
make local-compose-image-import IMAGE=myapp:dev
make kind-image-import IMAGE=myapp:dev
```

To exercise the registry-first path, push the image into the repo-managed local
registry instead:

```bash
make registry-up
make registry-image-push IMAGE=myapp:dev
```

The push target is `localhost:5001/...` from the host. The script also prints
the `host.docker.internal:5001/...` ref used by Axern runtime containers and
kind pods.

To build the repo-managed local Nydus smoke image without running a smoke:

```bash
make registry-up
make nydus-builder-image
make registry-nydus-image-build
```

Defaults and overrides:

- `NYDUS_SOURCE_IMAGE=axern/python311-runtime:dev`
- `NYDUS_LOCAL_IMAGE=localhost:5001/axern/nydus-smoke:dev`
- `NYDUS_IMAGE_REBUILD=1` forces conversion when the target already exists.
- Explicit `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` values are propagated to
  the Nydus builder image build. Axern does not probe or select a host proxy.
- For offline or unstable networks, place upstream release archives under
  `deploy/images/nydus-builder/cache/` using their original filenames, for
  example `nydus-static-v2.4.0-linux-arm64.tgz` and
  `buildkit-v0.24.0.linux-arm64.tar.gz`.

The repo-managed builder is the default smoke fixture path, so Axern's local
runtime verification does not require an external build service or another kind
cluster. Validate production-built Nydus images, including images produced by
Kova or another build pipeline, explicitly:

```bash
NYDUS_TEST_IMAGE=<registry/ref:tag-or-digest> make local-compose-nydus-smoke
NYDUS_TEST_IMAGE=<registry/ref:tag-or-digest> make kind-axern-nydus-smoke
```

The local workflows use these repo-built runtime and bundle images:

- `python311`: `axern/python311-runtime:dev`
- `server-base`: `axern/server-base-runtime:dev`
- `coding-base`: `axern/coding-base-runtime:dev`
- `desktop-base`: `axern/desktop-base-runtime:dev`
- `claude-code-bundle`: `axern/claude-code-bundle:dev`
- `codex-bundle`: `axern/codex-bundle:dev`

Bring-up and refresh flows rebuild these images and import them into the
node-local `imagemgr` cache, relying on Docker cache to keep the common
no-change path fast. Compose keeps that cache in a Docker-managed Linux volume
so extracted OCI layer ownership stays faithful to the image metadata.

## Endpoints

Compose:

- Gateway: `http://127.0.0.1:25080`
- Dashboard: `http://127.0.0.1:25080/dashboard?token=axern-local-dev`
- SSH terminal: `127.0.0.1:25022`
- Grafana LGTM: `http://127.0.0.1:13000`
- OTLP gRPC: `127.0.0.1:4317`
- OTLP HTTP: `127.0.0.1:4318`

Kind:

- Local registry: `http://127.0.0.1:5001`
- Gateway: `http://127.0.0.1:25082`
- Dashboard: `http://127.0.0.1:25082/dashboard?token=axern-local-dev`
- SSH terminal: `127.0.0.1:25023`
- Grafana LGTM: `http://127.0.0.1:13001`
- OTLP gRPC: `127.0.0.1:24317`
- OTLP HTTP: `127.0.0.1:24318`

Disable LGTM for leaner local runs:

```bash
OTEL=0 make local-compose-up
OTEL=0 make kind-up
```

## Cleanup

```bash
make local-compose-down
make local-compose-purge
make local-compose-reset

make kind-down
make registry-down
make kind-purge
make kind-reset
```

- `*-down` stops the environment but preserves repo-local state.
- `registry-down` stops only the Docker-backed local registry container.
- `*-purge` stops the environment and removes repo-local state.
- `*-reset` purges and immediately recreates the environment.
- `*-refresh` keeps the environment shell, resets runtime state, and redeploys.

## Proxy

Compose and kind reuse exported `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY`.
If a host proxy is explicitly exported through the standard proxy variables,
the bring-up and refresh scripts configure it for container or pod access via
`host.docker.internal`.

## Defaults

Local `node-all-in-one` capacity defaults:

- `AXNODED_MAX_INSTANCE_NUM=64`
- `AXNODED_INTERFACE_CACHE_SIZE=16`
- `AXNODED_CGROUP_CACHE_SIZE=16`

Override them when bringing an environment up if a test needs a larger local
node.
