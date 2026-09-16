# Axrun Acceptance

`make axrun-verify` runs unit tests, vet, formatting, and the deterministic local TaskSet smoke.

Production acceptance additionally requires a Linux Axern cluster, a registry, and Kova:

- Kova builds and returns typed OCI plus Nydus manifest digests;
- Axrun publishes and plans by immutable descriptor digest;
- Axrun downloads the immutable OCI payload once, safely captures selected task inputs, and resumes from frozen local state;
- each Axern-backed Episode uploads its selected allocation-local workspace through the public archive API; verifier/oracle inputs remain phase-gated;
- caller-supplied agent image, agent phase, reward, resume, and export complete through generic Axern image-mount, process, file, and archive APIs;
- terminal episode evidence records allocation identity and the caller-supplied agent image digest;
- configured Claude Code and Codex adapters can reach their provider from the sandbox execution network.

Performance measurements must record the exact environment, payload, concurrency, and absolute P50/P95 values. The current architecture does not promise node-local TaskSet COW, warm-attempt upload elimination, or a cross-environment latency improvement threshold.
