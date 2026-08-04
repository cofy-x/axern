# Scripts

Repository-level utility scripts live here when they serve a real Axern build, release, or migration workflow.

Template build and code generation scripts were intentionally removed from the platform skeleton.

Scripts:

- `devbox/devbox.sh` Builds, starts, stops, inspects, and enters the repo-local Linux devbox container.
- `devbox/node-dev-prepare.sh` Prepares the repo-local Linux runtime workspace under `.dev/`.
- `devbox/node-dev-ensure-dlv.sh` Installs or verifies the Delve debugger used by the Linux debug workflow.
- `devbox/sudo-go.sh` Runs Go through passwordless sudo while preserving the devbox Go environment and any user-provided proxy variables for root runtime daemons.
- `devbox/stack.sh` Starts, stops, restarts, and inspects the standalone source-development stack inside the Linux devbox: repo-local Postgres plus the Axern daemons running directly from source.
- `dev-db-reset.sh` Drops, recreates, and initializes a local Axern Postgres database from the latest `controld` schema SQL. This is a development reset tool, not a migration runner.
- `agent-doc-check.sh` Verifies repo-local Markdown links and checks that module-level agent contracts are indexed. Use it after changing repository Markdown.
- `axrun/local-smoke.sh` Verifies deterministic compilation of a local `axrun/v1` TaskSet bundle and its OCI layout.
- `gatewayd-architecture-check.sh` Verifies gatewayd package layout and dependency direction.
- `imagemgr-architecture-check.sh` Verifies imagemgr package layout and dependency direction.
- `verify-all.sh` Runs the repository validation pass serially: root lint/build/test, shared proto validation, `network/bpfnet` tests, plus the standardized `controld`, `axern` CLI, `bpfnetctl`, and `axnoded` E2E / verify entrypoints. `make -C network/bpfnet generate-check` is opt-in via `--include-bpfnet-generate-check` because it is slow and usually only matters when committed tc artifacts changed. `make local-storage-verify` is opt-in via `--include-local-storage` because it depends on live compose and kind truth environments. Use `--bootstrap` on a fresh machine and `--from <step>` to resume long runs.
- `dev-env/verify-local-storage.sh` Runs the compose and kind service-volume truth-path smokes with status snapshots and retry. Use it for storage state-machine, `volumed`, or storage diagnostics changes before broader verification.
- `benchmark-all.sh` Runs the repository benchmark flow serially using the stable `runtime/axnoded` Docker benchmark entrypoints. Add `--include-profiles` for perf-oriented profile steps. Production bpfnet performance validation belongs to the Kubernetes regression runbook under `network/bpfnet/docs/`.
- `dev-env/compose-tunnel-e2e.sh` Verifies the Axern raw TCP tunnel end to end in the local Docker Compose truth environment for both `runsc` and `runc`, using the same `python311` template with workload `--runtime-class` choices.
- `dev-env/compose-agent-bundle-matrix-smoke.sh` Runs the real Axern `ImageMount` smoke for Claude Code and Codex bundles against BusyBox 1.36 and Ubuntu 24.04 task rootfs images.
- `dev-env/kind-tunnel-e2e.sh` Verifies the same Axern raw TCP tunnel contract against the repo-managed kind truth environment. This is kept as an explicit entrypoint until the kind tunnel path is stable enough to gate every local truth run.
- `dev-env/kind-tunnel-relay-e2e.sh` Verifies the kind tunnel relay registry contract, including drain relay selection rejection, session-bound relay targets, peer events, and foreground connector reachability.
