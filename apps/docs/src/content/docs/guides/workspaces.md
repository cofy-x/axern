---
title: Persistent Workspaces
description: Retain files on one Linux host and reuse them from fresh isolated executions.
---

A Workspace is retained data with its own identity, Namespace, capacity and deletion. A Run can mount one Workspace at `/workspace`; only one Allocation may write it at a time. Images contain reproducible tools, while Agent tasks and sessions remain in your application.

:::caution[Experimental storage]

Use a coherent CLI, SDK and server build. The native host must explicitly enable an XFS project-quota budget. This feature retains files through cleaned execution replacement; supported backup, upgrades and replacement-host restore are not available. A source version does not establish published package availability.

:::

## Prepare the host

Follow the [native installation guide](https://github.com/cofy-x/axern/blob/main/deploy/standalone/INSTALL.md). Configure the dedicated XFS data mount with `prjquota`, a finite Workspace byte budget and a finite inode budget. Nodes with disabled or unavailable enforcement reject Workspace creation. The Compose development profile does not enable persistent storage implicitly.

`axern server` operates the installed Linux host. `axern dev` manages a separate Compose development instance. API contexts select the endpoint used by Workspace and Run operations; they do not select the host for `server` commands.

## Create and select data

Use the admitted Node ID from your installation configuration:

```bash
axern workspace create --node node-personal-01 --capacity 1Gi --inodes 100000
axern workspace list
axern workspace get <workspace-id>
axern run --workspace <workspace-id> --limit-memory 512Mi python:3.12-slim -- \
  python -c 'from pathlib import Path; Path("/workspace/note.txt").write_text("retained work\n")'
```

Creation waits for provisioning by default; `--wait=false` returns the initial observation. A Run's Workspace is immutable. A replacement Run receives a new Allocation ID and its own temporary rootfs, while the selected Workspace files remain.

## Continue from a fresh Sandbox

The Python API exposes the same control contract synchronously and through `AsyncAxernClient` / `AsyncSandbox`:

```python
from axern_sdk import AxernClient, Sandbox

client = AxernClient.from_context("/absolute/path/to/config.json")
workspace = client.create_workspace(
    node_id="node-personal-01", capacity_bytes=1024**3, inode_limit=100000,
)
client.wait_workspace_ready(workspace.id)

with Sandbox(client=client, image="python:3.12-slim", workspace_id=workspace.id, limit_memory="512Mi") as first:
    first.write_text("/workspace/note.txt", "retained work\n")

client.wait_workspace_ready(workspace.id)
with Sandbox(client=client, image="python:3.12-slim", workspace_id=workspace.id, limit_memory="512Mi") as second:
    assert second.read_text("/workspace/note.txt") == "retained work\n"

client.close()
```

Closing a Sandbox requests cancellation and preserves its Workspace. `wait_workspace_ready` waits for ready data and an empty attachment after confirmed cleanup. It reads observations, never retries a mutation and never reserves the next writer. A competing admission can still acquire the Workspace first. Go provides `WaitWorkspaceReady`; TypeScript provides `waitWorkspaceReady` with the same meaning.

Old process, file, SSH, terminal and Tunnel handles retain their original Allocation identity and cannot access the replacement. Files do not restore process memory, Agent conversation state or external effects.

## Capacity and deletion

The byte hard limit must be positive and aligned to 512 bytes; the inode limit must be positive. CLI quantities such as `1Gi` are accepted. Retained capacity remains charged while detached, unavailable or deleting, independently of the Run's ephemeral-rootfs charge. Project quota controls actual filesystem allocation; guest file sizes and sparse-file statistics are not the accounting authority. Limits do not reserve physical disk blocks against other host writers.

Namespace quota includes persistent byte and inode limits. Observe those limits with `axern quota get`; operators set them with `axern quota set`. Missing storage, changed ownership, withdrawn enforcement and full-disk conditions remain explicit failures. A missing directory never becomes an empty new Workspace.

```bash
axern workspace delete <workspace-id>
axern workspace get <workspace-id>
```

Deletion is explicit and asynchronous. Active or unresolved attachments block it. The charge ends only after exact data cleanup is confirmed and the Workspace reaches `deleted`; a failed delete retains its charge and diagnosis. Never use a development reset or manual directory removal as a serving maintenance operation.

The initial profile supports one writer, no concurrent read-only attachments, no arbitrary host paths, no resize and no shared multi-writer mounts. A Workspace cannot be combined with rootfs publication; bounded declared outputs remain available for explicit result handoff.
