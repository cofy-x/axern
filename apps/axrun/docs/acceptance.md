# Axrun Acceptance

`make axrun-verify` runs unit tests, vet, formatting, the local TaskSet smoke,
and the mandatory managed-rollout Compose E2E.

The Compose gate adds an independent local-only provider. It is not a
production provider enum, Helm option, or HK workload. Its black-box contract
covers OpenAI Responses and Anthropic Messages, provider failure classes,
stream failures, malformed responses, and missing usage. A scripted agent run
covers workspace mutation, verifier, reward, evidence, preflight metering,
resumable gateway download, and credential snapshot isolation across rotation.
Test model pricing is injected only into the test worker process.

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
- `profile doctor` and rollout preflight use a configured real provider/model
  from the rollout worker network.

Record absolute P50/P95 values per environment; enforce the relative gates
above instead of a cross-environment millisecond SLO.
