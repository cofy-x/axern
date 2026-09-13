# Axrun Acceptance

`make axrun-verify` runs unit tests, vet, formatting, and the deterministic local TaskSet smoke.

Production acceptance additionally requires a Linux Axern cluster, a registry, and Kova:

- Kova builds and returns typed OCI plus Nydus manifest digests;
- Axrun publishes and plans by immutable descriptor digest;
- Axrun downloads the immutable OCI payload once, safely captures selected task inputs, and resumes from frozen local state;
- caller-supplied agent image, agent phase, verifier upload, reward, resume, and export complete through generic Axern image-mount and archive APIs;
- terminal episode evidence records allocation identity, runtime class, and the caller-supplied agent image digest;
- configured Claude Code and Codex adapters can reach their provider from the sandbox execution network.

Record absolute P50/P95 values per environment; enforce the relative gates above instead of a cross-environment millisecond SLO.
