# Runtime Logs

Use this document to understand the critical runtime stack logs and what each
one means. It is environment-neutral: compose, kind, Helm, and future cloud
deployments should all map their log collection commands back to these same
components and node-local paths.

For local compose and kind commands, see
[Local Troubleshooting](../../deploy/local/troubleshooting.md).

## Core Logs

| Component | Log or stream | What to look for |
| --- | --- | --- |
| `controld` | process stdout/stderr | node registration, heartbeat freshness, node summary ingest, placement rejections, allocation dispatch, service/run state, gateway and tunnel target resolution |
| `storaged` | process stdout/stderr | Storage V1 volume class, claim, binding, and node volume resolution |
| `axnoded` | `/var/log/axnoded/axnoded.log` | control-plane registration/report failures, lifecycle RPCs, sandbox create/delete, OCI bundle generation, runtime command failures, cgroup setup, network setup |
| `node-tunneld` | `/var/log/axnoded/node-tunneld.log` | node-local tunnel agent restarts, allocation netns lookup, axnoded operator socket access, relay connection failures |
| `imagemgr` | `/var/lib/imagemgr/logs/imagemgr.log` | image import, `/oci_mount`, `/nydus_mount`, `/oss_mount`, overlay mount, Nydus bootstrap fetch, imagefsd daemon launch |
| `volumed` | process stdout/stderr | resolved volume publish/unpublish, provider validation, persistent publish state, reconcile health |
| `egressd` | process stdout/stderr | policy prepare/delete fencing, persistent recovery, orphan reconciliation, enforcement health |
| `imagefsd mount daemon` | `/var/lib/imagemgr/daemons/<daemon-id>/daemon.log` | OSS and Nydus image read path, backend fetches, cache/chunk behavior, FUSE mount daemon internals |
| `gatewayd` | process stdout/stderr | gateway route resolution, upstream connection failures, HTTP proxying, terminal and SSH forwarding |
| `tunneld` | process stdout/stderr | relay selection, peer/session pairing, relay drain behavior |
| `controld-migrate` | job/process stdout/stderr | Postgres schema migration failures |
| `postgres` | process stdout/stderr | database startup, readiness, connection failures |
| `minio` | process stdout/stderr | local object storage failures for OSS-style image tests |

## Node-Local Paths

These paths are inside the node runtime environment, such as the compose
`node` container, a kind `node-all-in-one` pod, or a future node-runtime host.

| Path | Meaning |
| --- | --- |
| `/tmp/axnoded-node-config.toml` | generated axnoded config used by node-all-in-one deployments |
| `/shared/run/axnoded.sock` | axnoded operator socket used by `axctl` and `node-tunneld` |
| `/run/imagemgr/imagemgr.sock` | axnoded-to-imagemgr image rootfs API socket |
| `/run/volumed/volumed.sock` | axnoded-to-volumed volume publish API socket |
| `/run/egressd/egressd.sock` | axnoded-to-egressd policy lifecycle API socket |
| `/var/lib/axnoded` | axnoded runtime state, store, rootfs, filestore |
| `/var/lib/imagemgr` | imagemgr state, logs, mount records, imagefsd daemon dirs |
| `/var/lib/volumed` | volumed state, local provider root, published volume records |
| `/var/lib/egressd` | egressd prepared policy records and recovery state |
| `/var/log/axnoded/axnoded.log` | axnoded daemon log |
| `/var/log/axnoded/node-tunneld.log` | node-tunneld supervisor log |

## Config Fields To Check

Inspect `/tmp/axnoded-node-config.toml` when socket paths, node identity,
runtime class, or image-manager settings look wrong.

| Field | Why it matters |
| --- | --- |
| `plugin.control_plane_target` | where axnoded registers and reports node state |
| `plugin.control_plane_node_id` | node identity shown in `controld` node summaries |
| `plugin.control_plane_node_target` | internal node address used by gateway/control-plane routing |
| `plugin.network.nat_backend` | `iptables` or `ebpf` network path |
| `plugin.runtime.image_manager_socket` | socket for image-backed rootfs requests |
| `plugin.runtime.volume_manager_socket` | socket for resolved node volume publish requests |
| `plugin.runtime.runtimes.runsc.binary` | gVisor runtime binary path |
| `plugin.runtime.runtimes.runc.binary` | runc runtime binary path |

## Symptom Map

| Symptom | Primary log chain |
| --- | --- |
| Environment is not healthy | `controld-migrate`, `postgres`, `controld`, node entrypoint logs |
| Workload is not scheduled | `controld` -> `axnoded` |
| Sandbox creation fails | `controld` -> `/var/log/axnoded/axnoded.log` -> `runsc` or `runc` errors |
| Service volume fails | `admin reliability check` -> `admin storage list --status failed` -> `controld` -> `storaged` -> `/var/log/axnoded/axnoded.log` -> `volumed` |
| Image or rootfs fails | `/var/log/axnoded/axnoded.log` -> `/var/lib/imagemgr/logs/imagemgr.log` -> `/var/lib/imagemgr/daemons/<daemon-id>/daemon.log` |
| Gateway HTTP or terminal fails | `gatewayd` -> `controld` -> `/var/log/axnoded/axnoded.log` |
| Tunnel fails | `tunneld` -> `controld` -> `/var/log/axnoded/node-tunneld.log` -> `/var/log/axnoded/axnoded.log` |

## Ownership Map

| Component | Owns |
| --- | --- |
| `controld` | node registration, placement, allocation dispatch, gateway/tunnel resolution |
| `storaged` | volume class, claim, binding, storage topology, resolved node volume specs |
| `axnoded` | node lifecycle, sandbox creation, runtime bundle, cgroup/network, operator socket |
| `volumed` | node-local physical volume providers, publish records, local cleanup |
| `egressd` | node-local egress policy persistence, recovery, reconciliation, and host enforcement |
| `imagemgr` | image import, image-backed rootfs orchestration, OCI overlay, Nydus/OSS daemon lifecycle |
| `imagefsd` | read-only image data, cache, chunk DB, mount daemon internals |
| `gatewayd` | service HTTP, terminal, SSH forwarding after route resolution |
| `tunneld` | relay-side tunnel session pairing |
| `node-tunneld` | node-local tunnel agent launch and allocation netns lookup |

For architecture context, see
[Runtime Architecture](../architecture/runtime-architecture.md) and
[Runtime Stack](../../.x/runtime-stack.md).
