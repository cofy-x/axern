---
title: Storage and Volumes
description: Attach persistent volumes to services and sandboxes through Axern's Storage V1 model.
---

Axern storage separates control-plane intent from node-local publish work.
`storaged` owns volume classes, claims, allocation bindings, and reclaim
policy; `volumed` materializes the published volume on the selected node.
Claims describe data intent and lifetime; bindings describe one allocation's
attachment and may be created, published, released, and retried many times
over the life of a claim.

The current provider is `local`, backed by a managed directory on the volume
node. A local volume pins recovery to its original node; a missing node is an
explicit storage-topology failure rather than a silent replacement with an
empty directory.

:::caution[Advanced API status]

Storage V1 exposes the control-plane model and mount intent, but the stable
`axern` CLI does not yet provide a user-facing VolumeClass/VolumeClaim
create/list/delete workflow. The `VolumeMount` example below is therefore not
a standalone volume provisioning recipe: it assumes that the named claim is
created by a platform-managed workflow, such as an agent workspace or a
service-owned claim.

:::

## Attach an existing volume from Python

Use `VolumeMount` to attach Service V1 volumes to a service-backed sandbox:

```python
from axern_sdk import AxernClient, Sandbox, VolumeMount

client = AxernClient.from_context("~/.config/axern/config.json")

with Sandbox(
    client=client,
    image="docker.io/library/python:3.12-slim",
    volumes=[
        VolumeMount("data", "/data"),
        VolumeMount("cache", "/cache", readonly=True, options=("rbind",)),
    ],
) as sandbox:
    result = sandbox.exec("ls /data /cache", text=True, check=True)
    print(result.stdout)
```

The SDK passes volume intent through the public control plane; storage
resolution, node publish, mount injection, and release remain platform-owned.
It does not create a missing claim from `VolumeMount("data", "/data")`.

## Where volumes appear

- **Coding agent workspaces** mount their persistent volume at
  `/home/axern/workspace`; data survives `agent stop` and resumes with the
  next session. See [Coding Agents](/guides/agent/).
- **Services** keep service-scoped data across allocation replacement and
  node-runtime restarts while their claim exists.

## Durability and recovery

With the current `local` provider, data survives allocation replacement and
node-runtime restart, but remains tied to its original node. Axern does not
replicate, fail over, or back up local volumes automatically. A missing node is
an explicit storage-topology failure rather than a replacement with an empty
directory.

## Reclaim and operator visibility

Releasing an allocation removes its binding but does not delete the claim or
its data. Reclaim policy is evaluated only when the claim is deleted. `Retain`
keeps the backend data and an auditable tombstone; `Delete` completes only
after the node confirms physical removal.

Use `axern admin storage list` to inspect bindings,
`axern admin storage reclaim list` to inspect pending physical deletion, and
`axern admin reliability check` for platform-level storage health.

For the ownership, lifecycle, and reclaim contract, see the repository's
[storage architecture](https://github.com/cofy-x/axern/blob/main/docs/architecture/storage-architecture.md).
