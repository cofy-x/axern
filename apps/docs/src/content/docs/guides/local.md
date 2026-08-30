---
title: Local Axern Reference
description: Requirements, lifecycle, storage, upgrades, and troubleshooting for Local Axern.
---

`axern local` manages one machine-level instance named `local`. Its deployment
assets and service versions come from the installed CLI; source checkout files
are never consulted. Release binaries embed a verified multi-architecture
image digest lock, so local startup does not resolve mutable service tags.

This page is the complete reference. For a guided first run, start with
[Local Axern](/getting-started/compose/).

## Requirements and boundaries

The first release supports macOS and Linux on amd64 and arm64 with Docker
Compose v2. Windows, Podman, offline installation, multiple local instances,
and automatic Kubernetes creation are not supported.

Recommended host capacity is 4 CPU cores, 8 GiB memory, and 20 GiB free disk.
Workload and agent images are pulled on first use. The optional observability
profile requires additional memory and disk.

## Commands

| Command | Behavior |
| --- | --- |
| `axern local up` | Preflight, materialize, start, wait for health, and configure the `local` context |
| `axern local image load IMAGE` | Stream a host Docker image into the local node; `--pull` fetches it first |
| `axern local status` | Show versions, health, Dashboard, data path, context, and disk use |
| `axern local logs [component]` | Read aggregate or component logs; supports `--follow`, `--tail`, and `--since` |
| `axern local doctor` | Perform read-only host, Docker, port, version, health, and Node DNS checks; `--probe` also verifies sandbox DNS |
| `axern local down` | Remove containers and network while preserving data |
| `axern local reset` | Permanently delete data and identity material |
| `axern local upgrade` | Back up and explicitly migrate to the CLI's stack version |
| `axern local path` | Print the effective local data directory |

Use `axern local up --profile observability` to enable local telemetry and
`axern local up --profile default` to return to the core profile. Omitting the
flag preserves the instance's current profile.

Image loading is local-only. It saves the immutable host image ID, validates
its platform against the running node image, and streams it directly into the
node without a host-side archive. Rebuilding the same mutable tag moves new
allocations to the new manifest generation; running allocations retain their
leased generation. The `local` CLI context records only the current mutable
tag pointer and submits its immutable digest to the control plane, so creating
a Run or Service from a loaded image does not contact an external registry.

## Data paths

| Platform | Default |
| --- | --- |
| macOS | `~/Library/Application Support/Axern/local` |
| Linux | `${XDG_DATA_HOME:-~/.local/share}/axern/local` |

Set `AXERN_HOME` to place all Axern-managed local data under a different root.
The CLI stores generated certificates, SSH keys, Compose materialization,
secrets, database/object data, metadata, and upgrade backups there. Sensitive
files are written with owner-only permissions.

## Local ports

All host listeners bind only to `127.0.0.1`.

| Port | Purpose |
| --- | --- |
| `25000` | Public gRPC gateway |
| `25080` | Dashboard and HTTP gateway |
| `25022` | Gateway SSH |
| `24101` | Control-plane HTTP |
| `25432` | PostgreSQL |
| `29000` / `29001` | MinIO API / console |

The observability profile additionally uses `4317`, `4318`, and `13000`.
The first version intentionally does not allocate alternate ports; stop the
conflicting process and rerun `axern local doctor`.

## Context behavior

`local up` creates or updates a context named `local` with absolute paths to
the generated mTLS and SSH identity. It selects that context only when no
context is active. Use `--use` to switch explicitly. Existing non-local
contexts are preserved.

## Proxy behavior

The runner passes `HTTP_PROXY` and `HTTPS_PROXY` into containers and rewrites
loopback proxy hosts to `host.docker.internal`. Internal Axern service names,
loopback, private networks, and cluster-local names are included in
`NO_PROXY`. If image pulls fail, first confirm Docker itself is configured for
your network, then run:

```bash
axern local doctor
axern local logs node --tail 200
```

## Workload DNS

By default, axnoded derives non-loopback resolver IPs from the Node container's
effective resolver configuration for OCI workloads. Docker's container-local
loopback resolver is not copied into a nested sandbox; when Docker publishes
external upstream metadata, axnoded uses those reachable addresses instead.
`axern local doctor` validates the resolver configuration actually applied to
an initialized stack (`runtime_dns_config`) and queries each effective resolver
directly from the running Node container (`runtime_dns_node`). An empty
materialized override means axnoded derives the resolver from the Node; it is
not an invalid configuration. Both checks are read-only and use a 15-second timeout by default;
change it with `--check-timeout`.

To verify the same DNS materialization through a real `runsc` OCI sandbox, run:

```bash
axern local doctor --probe
```

The sandbox check (`runtime_dns_sandbox`) uses the public Namespace, Secret,
Environment, and Run APIs. Cleanup cancels an active Run, then deletes the
Environment, Secret, and Namespace in dependency order after success, failure,
timeout, or cancellation. The terminal Run remains as normal control-plane
history. The default query is the project-controlled absolute name
`axern.cofy-x.space.`. Managed-network users can select a private name with
`--dns-query-name`; the value is injected through the temporary Secret and is
not returned in doctor JSON details or Run arguments.

The probe always connects to the product-owned `local` context and ignores a
currently selected remote context. Explicit remote endpoint or TLS overrides
are rejected. Sandbox execution defaults to five minutes; adjust it with
`--probe-timeout`. The defaults are template `python311` and runtime class
`runsc`; sandbox-only options require `--probe`. A cleanup failure is a
required failure and should be remediated by inspecting probe-labeled local
resources before retrying.

VPNs and managed networks sometimes require a resolver that is not visible in
the Node container's effective configuration. Set an explicit comma-separated list before
starting or recreating the local stack:

```bash
AXERN_LOCAL_DNS_NAMESERVERS=10.0.0.53,10.0.0.54 axern local up
```

Values must be IP addresses reachable from Docker workloads. Loopback,
unspecified, empty, and hostname values are rejected. After changing resolver
settings for a running instance, use `axern local down` followed by `axern
local up` to recreate the Node container while preserving data.

## Version changes

`local up` never silently changes a stack version. A mismatch tells you to run
`axern local upgrade`; status, logs, doctor, and down remain available.

Upgrade stops the old stack, creates a timestamped backup of data, identities,
metadata, and deployment files, applies the supported migration, then verifies
health. Downgrades are rejected. If no supported migration exists, reset the
local instance explicitly.

## Uninstall

Stop and delete the local instance before uninstalling the CLI if you no longer
need its data:

```bash
axern local reset
brew uninstall axern
```

When installed by the shell installer, remove the `axern` binary from the
directory printed during installation.
