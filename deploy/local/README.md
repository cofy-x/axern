# Contributor Local Deployment

These repository workflows are for Axern contributors building the current checkout. Product users should install the release CLI and run `axern dev up`; they do not need this directory, Make, or source-built images.

This directory contains the repo-supported local truth environments:

- `compose/`: Docker Compose deployment
- `kind/`: repo-managed 3-node kind cluster with one explicitly pinned Axern runtime node
- `k8s/`: manifests shared by the kind flow
- `state/`: generated local PKI, CLI env files, SSH keys, and runtime state

The source Compose workflow owns the `axern-source` Docker Compose project. The released CLI owns the separate `axern-local` project and its state under the platform application-data directory. Public commands such as `axern run`, `axern ssh`, and `axern tunnel` can use either configured context, but `axern dev ...` manages only the released-CLI instance and must not be mixed with this source workflow.

For gateway, terminal, and SSH examples, see [Gateway Quickstart](gateway-quickstart.md).

## Deployment Scope

Linux single-host serving is the product's deployment direction; macOS supports clients and local development with the Linux runtime inside a VM. The current `axern dev up` and source Compose workflows run multiple services, including PostgreSQL. They remain development environments rather than the planned serving package described in [Single-Host Agent Serving](../../docs/decisions/single-host-agent-serving.md).

Both Compose workflows use machine-local development identities and loopback public listeners. Their node configuration sets `AXNODED_CGROUP_ENFORCEMENT=disabled_dev`, so successful local execution does not qualify production CPU and memory enforcement. Keep runtime isolation, resource qualification, and deployment readiness distinct; use the [verification tiers](../../docs/verification/local-full-verification.md) for the applicable Linux checks.

## Start

```bash
make local-compose-up
make kind-up
```

Both flows build local deploy images before starting services. They also write CLI env files and refresh the local `axern` context config:

- `deploy/local/state/compose/axern.env`
- `deploy/local/state/kind/axern.env`

The kind node reserves 1 GiB for node daemons and runtime conformance, matching the verification runtime default. A 512 MiB reserve cannot cover both the 512 MiB conformance charge and the daemons: capability checks correctly fail closed and prevent workload placement. This local test reserve is not a production sizing recommendation; production uses measured qualification evidence.

`make kind-up` also starts or reuses a Docker-backed repo-managed local registry at `127.0.0.1:5001`. New repo-managed kind clusters mirror `localhost:5001` to that registry through the Docker `kind` network. If an existing kind cluster was created before this mirror existed or before the registry container was named `axern-registry`, run `make kind-reset` to recreate it.

The source build includes qualified PostgreSQL and Registry recipes; packaged development uses the matching coordinated release images and digest lock. See the [Registry recipe](../images/registry/README.md) and [PostgreSQL recipe](../images/postgres/README.md). Reusing a registry container requires the selected image identity to match. Preserve its existing `/var/lib/registry` volume when explicitly recreating it with a changed image; the start command refuses a mismatched container instead of silently using an older image.

Registry defaults:

- `LOCAL_REGISTRY_NAME=axern-registry`
- `LOCAL_REGISTRY_PORT=5001`
- `LOCAL_REGISTRY_IMAGE=axern/local-registry:dev`
- `LOCAL_REGISTRY_HOST=localhost:5001`
- `LOCAL_REGISTRY_CLUSTER_HOST=host.docker.internal:5001`

Useful status commands:

```bash
make local-compose-status
make kind-status
```

For runtime stack failures, use the compose/kind commands in [Local Troubleshooting](troubleshooting.md).

For kind kube access:

```bash
eval "$(make kube-env-kind)"
kubectl get nodes
```

## Daily Verification

Use the refresh path for normal development after both environments already exist:

```bash
make local-refresh-verify
```

This runs:

```bash
make local-compose-refresh-verify
make kind-refresh-verify
```

Refresh verification keeps the existing compose project or kind cluster, rebuilds platform images, resets the environment database/state, reruns migrations, redeploys core services, imports explicit test workload images, and runs the core smoke suites.

Use targeted refreshes when you only need one environment:

```bash
make local-compose-refresh-verify
make kind-refresh-verify
```

Use the clean-slate truth check before larger handoffs or when environment state itself is suspect:

```bash
make local-truth-verify
```

`local-truth-verify` purges and recreates compose and kind, then runs the full local smoke suite.

## Targeted Smoke

```bash
make local-compose-smoke
make local-compose-run-smoke
make local-compose-quota-smoke
make local-compose-computer-use-e2e

make kind-smoke
make kind-run-smoke
make kind-quota-smoke
```

Resource admission, quota, request/limit, or node inventory changes should at least run:

```bash
make local-compose-smoke
make local-compose-run-smoke
make local-compose-quota-smoke
make kind-smoke
make kind-run-smoke
make kind-quota-smoke
```

Tunnel-specific changes should also run:

```bash
make local-compose-python-sdk-e2e
```

The Python SDK E2E creates a Run-backed sandbox and verifies the tunnel session, relay pairing, data flow, renewal, and revocation lifecycle.

Image-backed Run checks are intentionally separate because registry-first paths and optional external image overrides can depend on registry or proxy reachability:

```bash
make local-compose-registry-image-smoke
make local-compose-image-mount-smoke
make kind-axern-registry-image-smoke
```

- `local-compose-registry-image-smoke` pushes a local runtime image into the repo-managed local registry and starts an Axern run from the registry ref through the compose node runtime path.
- `local-compose-image-mount-smoke` pushes a task image and a tiny reusable image bundle into the repo-managed local registry, starts an Axern run with `--image-mount`, and verifies the mounted bundle is executable and read-only.
- `kind-axern-registry-image-smoke` pushes a local runtime image into the repo-managed local registry and starts an Axern run from the registry ref.


The node entrypoint probes imagemgr inventory and egressd health over their real APIs before starting axnoded. Socket files can survive a crash and are not readiness evidence. Dependency checks have bounded calls and fail when the owned child exits or the startup deadline expires.

Refresh verification can opt into those broader checks:

```bash
COMPOSE_REFRESH_TUNNEL_E2E=1 make local-compose-refresh-verify
COMPOSE_REFRESH_TUNNEL_BENCHMARK=1 make local-compose-refresh-verify
COMPOSE_REFRESH_IMAGE_SERVICE_SMOKE=1 make local-compose-refresh-verify
LOCAL_COMPOSE_REGISTRY_IMAGE_SMOKE=1 make local-compose-refresh-verify
KIND_REFRESH_TUNNEL_E2E=1 make kind-refresh-verify
KIND_REFRESH_TUNNEL_RELAY_E2E=1 make kind-refresh-verify
KIND_REFRESH_TUNNEL_MULTIRELAY_E2E=1 make kind-refresh-verify
KIND_REFRESH_IMAGE_SERVICE_SMOKE=1 make kind-refresh-verify
KIND_REFRESH_REGISTRY_IMAGE_SMOKE=1 make kind-refresh-verify
```

`make local-truth-verify` is the clean full pass and runs the tunnel e2e, benchmark, relay registry, and multi-relay checks by default after rebuilding local images and resetting compose/kind.

## Local Images

Build local images without starting an environment:

```bash
make local-images-build
```

To use a host-built image that is not available from a registry, import it into the node image cache first:

```bash
docker build -t myapp:dev .
make local-compose-image-import IMAGE=myapp:dev
make kind-image-import IMAGE=myapp:dev
```

To exercise the registry-first path, push the image into the repo-managed local registry instead:

```bash
make registry-up
make registry-image-push IMAGE=myapp:dev
```

The push target is `localhost:5001/...` from the host. The script also prints the `host.docker.internal:5001/...` ref used by Axern runtime containers and kind pods.

Axern builds and publishes only platform images. Workload images are caller-owned explicit inputs. Targeted E2E checks may build narrowly scoped test fixtures and import them into the node-local `imagemgr` cache; those fixtures are neither preloaded nor release artifacts. Compose keeps the image cache in a Docker-managed Linux volume so extracted OCI layer ownership stays faithful to image metadata.

## Endpoints

Compose:

- Gateway: `http://127.0.0.1:25080`
- SSH terminal: `127.0.0.1:25022`

Kind:

- Local registry: `http://127.0.0.1:5001`
- Gateway: `http://127.0.0.1:25082`
- SSH terminal: `127.0.0.1:25023`

## Telemetry

Compose and kind run only the Axern development services and their core dependencies. Telemetry backends are operator-owned. To export to a receiver reachable from the containers or Pods, set the same explicit environment when starting or refreshing the environment:

```bash
AXERN_OTEL_ENABLED=true OTEL_EXPORTER_OTLP_ENDPOINT=https://telemetry.example:4317 make local-compose-up
AXERN_OTEL_ENABLED=true OTEL_EXPORTER_OTLP_ENDPOINT=https://telemetry.example:4317 make kind-up
```

Export is disabled by default. An enabled exporter requires an endpoint; An HTTPS endpoint uses TLS; an explicit `http://` URL or `OTEL_EXPORTER_OTLP_INSECURE=true` selects plaintext and should only address a trusted receiver. These workflows do not install a Collector, open telemetry listeners, or manage telemetry retention. Static Grafana dashboards are available under `deploy/helm/axern/files/grafana` for an independently managed backend.

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

Compose and kind reuse exported `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY`. If a host proxy is explicitly exported through the standard proxy variables, the bring-up and refresh scripts configure it for container or pod access via `host.docker.internal`.

## Defaults

Packaged nodes enable only gVisor (`runsc`). Python/Go SDK, CLI, and tunnel E2E entrypoints use runsc by default. The runtime binaries retained for explicit low-level diagnostics do not enable additional production runtimes.

Local `node-all-in-one` capacity defaults:

- `AXNODED_MAX_INSTANCE_NUM=64`
- `AXNODED_INTERFACE_CACHE_SIZE=16`
- `AXNODED_CGROUP_CACHE_SIZE=16`

Override them when bringing an environment up if a test needs a larger local node.

## Local Node Identity

The single local Kubernetes runtime uses the explicit `node-k8s-local` identity, independent of the host running the migration Job. Kind pins it to `<cluster-name>-worker`; other multi-node local clusters must set `AXERN_LOCAL_RUNTIME_NODE`. Use Helm explicit per-host identity bindings for multiple Axern runtime nodes. Bootstrap tokens mount read-only and are not copied into configuration or environment contents. Never reuse these development identities across independent deployments.
