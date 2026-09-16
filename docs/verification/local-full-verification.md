# Verification Tiers

Verification is layered by feedback time and evidence strength. Local feedback, Linux correctness, full repository regression, and environment qualification have different owners and must not be serialized into every edit-and-merge loop. Run each required tier on the same commit and worktree.

| Tier                             | Expected feedback budget            | Merge role                                   |
| :------------------------------- | :---------------------------------- | :------------------------------------------- |
| Changed host-safe checks         | 5 minutes on a warm workspace       | Normal local gate                            |
| Selected Linux/local integration | 15 minutes for the standard CI path | Affected PR gate                             |
| Full repository regression       | 45 minutes                          | Broad-change or asynchronous post-merge gate |
| Environment qualification        | No PR feedback budget               | Frozen-candidate release/promotion gate      |

Treat these as maintainability budgets, not timeouts that weaken correctness. When a tier repeatedly exceeds its budget, split its smoke feedback from its complete evidence workflow instead of reducing the latter's sample or coverage.

## Tier 1: changed and host-safe checks

Use the change planner as the normal repository entrypoint. It prints its scope before running and never starts Compose, kind, privileged Linux, or environment qualification:

```bash
make verify-changed-plan
make verify-changed
```

Set `VERIFY_BASE=<ref>` when the comparison base is not `origin/main`. For a broad or unclassifiable source change, the planner fails safe to `make verify-fast-all`, the complete host-safe source gate. Protobuf generated output checks finish before any Go compilation.

Workflow, verification orchestration, and shared Docker build-cache changes also select `make release-check`. These paths can invalidate release and image-build contracts even without modifying release artifacts; local validation must execute those same contracts before PR CI.

Tier 1 must not start real OOM, disk-fill, or sampled performance workloads. Its normal budget is minutes. A normal pull request may merge after its selected Tier 1 checks and GitHub checks pass; it does not also require a local full gate.

Concurrency unit tests must prove overlap, ordering, and limits through explicit synchronization rather than total elapsed-time thresholds. Wall-clock deadlines may bound a stuck test, but shared-runner speed is not a correctness assertion. Release all test barriers and join workers before closing their stores or removing temporary directories.

## Tier 2: affected Linux and local integration

Use the Linux devbox or a narrow privileged Docker verification only for an affected runtime boundary:

```bash
make -C runtime/axnoded verify-docker-runsc
make -C runtime/axnoded verify-network-policy-linux-smoke
```

Prefer the narrow runsc, sandboxd, rootfs, cgroup, XFS, EROFS, or network policy target when a change does not cross several boundaries. A broad runtime or deployment change may also run:

```bash
make local-compose-refresh-verify
make kind-refresh-verify
```

The repository change planner emits `network_policy_linux` and heavyweight scopes. Pull-request CI uses those outputs to keep stable check names while avoiding unrelated heavyweight work. Linux CI is authoritative for namespace, cgroup, mount, eBPF, runsc behavior; macOS is not expected to duplicate it.

The required `Go` check also runs `make axnoded-test` on Linux before merge. This executes the complete node package test suite, including Linux-only ownership and startup fixtures. Neither `test-host` nor the selected network traffic matrix substitutes for this suite. Post-merge regression repeats it as part of the broader integration sequence.

Compose DNS verification uses a repository-owned authoritative fixture over UDP and TCP. The refresh materializes that fixture as the explicit node resolver and verifies config, node, and real OCI sandbox DNS paths without host-resolver or public-resolver fallback.

Tier 2 proves correctness with a small deterministic matrix. Sampling must not turn it into an hours-long qualification. If performance evidence is needed, keep the correctness smoke and qualification as distinct profiles and outputs.

## Tier 3: asynchronous full repository gate

`make verify-full` is the broad-change and post-merge repository gate. It is not a default prerequisite for every pull-request merge or a command to restart after each edit. On failure, first reproduce the named step directly, then use the printed `--from <step>` resume point while diagnosing:

```bash
make verify-full
make verify-full ARGS='--include-bpfnet-generate-check'
make verify-full ARGS='--include-proto-breaking'
```

The `Post-Merge Full` GitHub workflow triggers the complete default gate for every `main` commit and supports manual dispatch. It partitions the same ordered steps into `make verify-full ARGS='--suite source'` (through Linux node unit tests) and `make verify-full ARGS='--suite runtime'` (CLI and all runtime E2E). The suites run on separate runners; runtime tests remain serial because they share Docker, network, and privileged host resources. A contract test requires their ordered union to equal the default gate exactly, without duplicates. `Full Repository Regression` succeeds only when both suites succeed; failures do not cancel the other suite.

The workflow uses read-only repository permissions, upstream package sources, language and Docker build caches, a three-hour safety timeout per suite, and one workflow concurrency slot per ref. Regional acceleration belongs in the external workspace entrypoint. When a newer `main` commit arrives, the older in-progress run is cancelled; a cancelled run is not passing evidence. Every commit triggers verification, but not every superseded commit completes it.

Each suite publishes a summary plus a 14-day artifact containing the exact commit, run identity, suite, bootstrap log, regression log, stage durations and exit codes, and failure diagnostics when applicable. Set `VERIFY_TIMINGS_FILE` to an output file with an existing parent directory to collect the same tab-separated timing report locally. Completed and failed stages are recorded; an abruptly killed runner may leave only completed stages, and the job outcome still determines success. GitHub's job log remains the authoritative live view. Rerunning a remote suite starts from a clean runner; `--from` remains a same-workspace local diagnosis aid and cannot be combined with a named suite.

During a serial gate, Docker builds still validate all inputs through BuildKit, including dirty local sources. A run-local export receipt suppresses repeated GitHub cache exports only after the resulting image ID has already been exported successfully to that scope. Changed images and failed exports are not treated as cache hits. The first export uses a second cached BuildKit solve without loading another image; later stages retain their normal build and load path. Receipts are removed when the gate exits and never authorize image reuse or skip tests. Standalone build commands retain their combined build/export behavior. Compare cold- and warm-cache runs before claiming a speedup; neither a cache receipt nor a fast smoke is release evidence.

Do not release or promote a commit until its exact `Full Repository Regression` job succeeds. The workflow is deliberately not a required pull-request check.

Use `make verify-release` for the full repository gate with Axrun acceptance. Deployment and environment qualification remain separate; this command does not produce an environment qualification receipt.

## Tier 4: environment qualification

Digest-pinned memory, network-policy performance, capacity, soak, regional rollout, and deployed acceptance run only for a frozen release candidate or an explicitly scoped investigation. They must execute and collect remotely without depending on a live developer shell.

A reduced remote smoke may provide correctness feedback, but it must have a distinct profile and output and can never be presented as qualification or promotion evidence. Full qualification preserves its existing sample counts, budgets, immutable artifact identity, environment identity, and receipt checks.

The final `git status --short` must contain only intentional changes. The handoff records the exact commands and results for the tiers that were required, rather than claiming tiers that were skipped by scope.
