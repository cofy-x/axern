# Runtime Logs

Use this document to understand the critical runtime stack logs and what each one means. It is environment-neutral: compose, kind, Helm, and future cloud deployments should all map their log collection commands back to these same components and node-local paths.

For local compose and kind commands, see [Local Troubleshooting](../../deploy/local/troubleshooting.md).

## Core Logs

| Component | Log or stream | What to look for |
| --- | --- | --- |
| `controld` | process stdout/stderr | node registration, heartbeat freshness, node summary ingest, placement rejections, allocation dispatch, Run/Allocation state, gateway and tunnel target resolution |
| `axnoded` | `/var/log/axnoded/axnoded.log` | control-plane registration/report failures, lifecycle RPCs, sandbox create/delete, OCI bundle generation, runtime command failures, cgroup setup, network setup |
| `node-tunneld` | `/var/log/axnoded/node-tunneld.log` | node-local tunnel agent restarts, allocation netns lookup, axnoded operator socket access, relay connection failures |
| `imagemgr` | `/var/lib/imagemgr/logs/imagemgr.log` | image import, `/oci_mount`, `/nydus_mount`, overlay mount, Nydus bootstrap fetch, imagefsd daemon launch |
| `egressd` | process stdout/stderr | policy prepare/delete fencing, persistent recovery, orphan reconciliation, enforcement health |
| `imagefsd mount daemon` | `/var/lib/imagemgr/daemons/<daemon-id>/daemon.log` | Nydus image read path, backend fetches, cache/chunk behavior, FUSE mount daemon internals |
| `gatewayd` | process stdout/stderr | gateway route resolution, upstream connection failures, HTTP proxying, terminal and SSH forwarding |
| `tunneld` | process stdout/stderr | relay selection, peer/session pairing, relay drain behavior |
| `controld-migrate` | job/process stdout/stderr | Postgres schema migration failures |
| `postgres` | process stdout/stderr | database startup, readiness, connection failures |

## Node-Local Paths

These paths are inside the node runtime environment, such as the compose `node` container, a kind `node-all-in-one` pod, or a future node-runtime host.

| Path                                | Meaning                                                      |
| ----------------------------------- | ------------------------------------------------------------ |
| `/tmp/axnoded-node-config.toml`     | generated axnoded config used by node-all-in-one deployments |
| `/shared/run/axnoded.sock`          | axnoded operator socket used by `axctl` and `node-tunneld`   |
| `/run/imagemgr/imagemgr.sock`       | axnoded-to-imagemgr image rootfs API socket                  |
| `/run/egressd/egressd.sock`         | axnoded-to-egressd policy lifecycle API socket               |
| `/var/lib/axnoded`                  | axnoded runtime state, store, rootfs, filestore              |
| `/var/lib/imagemgr`                 | imagemgr state, logs, mount records, imagefsd daemon dirs    |
| `/var/lib/egressd`                  | egressd authoritative prepared policy records                |
| `/var/log/axnoded/axnoded.log`      | axnoded daemon log                                           |
| `/var/log/axnoded/node-tunneld.log` | node-tunneld supervisor log                                  |

## Config Fields To Check

Inspect `/tmp/axnoded-node-config.toml` when socket paths, node identity, runsc configuration, or image-manager settings look wrong.

| Field                                  | Why it matters                                              |
| -------------------------------------- | ----------------------------------------------------------- |
| `plugin.control_plane_target`          | where axnoded registers and reports node state              |
| `plugin.control_plane_node_id`         | node identity shown in `controld` node summaries            |
| `plugin.control_plane_node_target`     | internal node address used by gateway/control-plane routing |
| `plugin.network.nat_backend`           | `iptables` or `ebpf` network path                           |
| `plugin.runtime.image_manager_socket`  | socket for image-backed rootfs requests                     |
| `plugin.runtime.runsc.binary`         | gVisor runtime binary path                                  |

## Symptom Map

| Symptom                            | Primary log chain                                                                                                             |
| ---------------------------------- | ----------------------------------------------------------------------------------------------------------------------------- |
| Environment is not healthy         | `controld-migrate`, `postgres`, `controld`, node entrypoint logs                                                              |
| Workload is not scheduled          | `controld` -> `axnoded`                                                                                                       |
| Sandbox creation fails             | `controld` -> `/var/log/axnoded/axnoded.log` -> `runsc` errors                                                                |
| Writable rootfs or workspace fails | `controld` -> `/var/log/axnoded/axnoded.log` -> `runsc` and node-local filestore errors                                       |
| Image or rootfs fails              | `/var/log/axnoded/axnoded.log` -> `/var/lib/imagemgr/logs/imagemgr.log` -> `/var/lib/imagemgr/daemons/<daemon-id>/daemon.log` |
| Gateway HTTP or terminal fails     | `gatewayd` -> `controld` -> `/var/log/axnoded/axnoded.log`                                                                    |
| Tunnel fails                       | `tunneld` -> `controld` -> `/var/log/axnoded/node-tunneld.log` -> `/var/log/axnoded/axnoded.log`                              |

## Ownership Map

| Component      | Owns                                                                                                                 |
| -------------- | -------------------------------------------------------------------------------------------------------------------- |
| `controld`     | node registration, placement, allocation dispatch, gateway/tunnel resolution                                         |
| `axnoded`      | node lifecycle, sandbox creation, runtime bundle, allocation-local writable storage, cgroup/network, operator socket |
| `egressd`      | node-local egress policy persistence, recovery, reconciliation, and host enforcement                                 |
| `imagemgr`     | image import, image-backed rootfs orchestration, OCI overlay, Nydus daemon lifecycle                                 |
| `imagefsd`     | read-only image data, cache, chunk DB, mount daemon internals                                                        |
| `gatewayd`     | Allocation-bound process, file, archive, terminal, SSH, Tunnel, and sandbox forwarding after target resolution       |
| `tunneld`      | relay-side tunnel session pairing                                                                                    |
| `node-tunneld` | node-local tunnel agent launch and allocation netns lookup                                                           |

For architecture context, see [Runtime Architecture](../architecture/runtime-architecture.md) and [Runtime Stack](../../.x/runtime-stack.md).
