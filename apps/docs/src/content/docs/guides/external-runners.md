---
title: External Runners
description: Build inference and verification workflows using only Axern's released public SDK.
---

Axern owns environment execution. An external runner owns episodes, agent policy, CandidateBundle and VerificationResult schemas, trajectories, scoring, and durable publication. It persists only public `environment_id`, `run_id`, and `allocation_id` values; Node, runtime, lease, access-grant, and routing identities remain internal.

The recoverable path is:

```text
resolved input
  -> Environment
  -> inference Run / Allocation
  -> sealed declared candidate
  -> fresh verification Run / Allocation
  -> sealed declared verification result
  -> caller-owned durable store
```

Declare bounded outputs in the immutable Run specification. The node quiesces the Allocation and atomically seals regular files or directory-as-tar outputs before runtime deletion. Query the manifest by `run_id`, verify a full download's size and SHA-256, then copy the accepted bytes to caller-owned storage before the 15-minute expiry. Node-disk loss remains an explicit unavailable result.

Inference and verification never share a writable filesystem. Upload the downloaded candidate into a new verifier Allocation using the public file or archive APIs. SSH, Terminal, and Tunnel remain diagnostic or interactive connections and are not durable result channels.

Limits are 16 paths, 64 MiB per file, 256 MiB per tar archive, and 256 MiB combined. Missing, rejected, capture-failed, and node-unavailable outputs are manifest states rather than partial success. Axern does not interpret candidate or verifier schemas and does not provide an Artifact service, Dataset registry, persistent Workspace, or object store.

See the repository's [external runner integration contract](https://github.com/cofy-x/axern/blob/main/docs/product/external-runner-integration.md) for recovery and process-stream semantics.
