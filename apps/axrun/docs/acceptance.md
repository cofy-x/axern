# Axrun Acceptance

`make axrun-verify` runs unit tests, vet, formatting, and the deterministic
local TaskSet smoke.

Production acceptance additionally requires a Linux Axern cluster with
imagemgr/imagefs, a registry, and Kova:

- Kova builds and returns typed OCI plus Nydus manifest digests;
- Axrun publishes and plans by immutable descriptor digest;
- axnoded prefers Nydus, falls back to OCI, and creates isolated COW uppers;
- mounted agent bundle, agent phase, verifier materialization, reward, resume,
  and export complete without client workspace `UploadArchive`;
- terminal episode evidence records the node-selected payload format and digest,
  cache result, image resolution/pull time, COW preparation time, verifier
  materialization time, allocation identity, runtime class, and agent digest;
- warm client-to-node input bytes improve by at least 95% and workspace-ready
  P95 improves by at least 50% against the tar-upload baseline.
- configured Claude Code and Codex adapters can reach their provider from the
  sandbox execution network.

Record absolute P50/P95 values per environment; enforce the relative gates
above instead of a cross-environment millisecond SLO.
