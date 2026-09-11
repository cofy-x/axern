# Verification Tiers

Verification is layered by feedback time and evidence strength. Local feedback,
Linux correctness, full repository regression, and environment qualification
have different owners and must not be serialized into every edit-and-merge loop.
Run each required tier on the same commit and worktree.

| Tier | Expected feedback budget | Merge role |
| :--- | :--- | :--- |
| Changed host-safe checks | 5 minutes on a warm workspace | Normal local gate |
| Selected Linux/local integration | 15 minutes for the standard CI path | Affected PR gate |
| Full repository regression | 45 minutes | Broad-change or asynchronous post-merge gate |
| Environment qualification | No PR feedback budget | Frozen-candidate release/promotion gate |

Treat these as maintainability budgets, not timeouts that weaken correctness.
When a tier repeatedly exceeds its budget, split its smoke feedback from its
complete evidence workflow instead of reducing the latter's sample or coverage.

## Tier 1: changed and host-safe checks

Use the change planner as the normal repository entrypoint. It prints its scope
before running and never starts Compose, kind, privileged Linux, or environment
qualification:

```bash
make verify-changed-plan
make verify-changed
```

Set `VERIFY_BASE=<ref>` when the comparison base is not `origin/main`. For a
broad or unclassifiable source change, the planner fails safe to
`make verify-fast-all`, the complete host-safe source gate. Protobuf generated
output checks finish before any Go compilation.

Tier 1 must not start real OOM, disk-fill, or sampled performance workloads. Its
normal budget is minutes. A normal pull request may merge after its selected
Tier 1 checks and GitHub checks pass; it does not also require a local full gate.

## Tier 2: affected Linux and local integration

Use the Linux devbox or a narrow privileged Docker verification only for an
affected runtime boundary:

```bash
make -C runtime/axnoded verify-docker-runsc
make -C runtime/axnoded verify-network-policy-linux-smoke
```

Prefer the narrow runc, runsc, sandboxd, rootfs, cgroup, XFS, EROFS, or network
policy target when a change does not cross several boundaries. A broad runtime
or deployment change may also run:

```bash
make local-compose-refresh-verify
make kind-refresh-verify
```

The repository change planner emits `network_policy_linux` and
`managed_rollout` scopes. Pull-request CI uses those outputs to keep stable check
names while avoiding unrelated heavyweight work. Linux CI is authoritative for
namespace, cgroup, mount, eBPF, runc, and runsc behavior; macOS is not expected
to duplicate it.

Compose DNS verification uses a repository-owned authoritative fixture over
UDP and TCP. The refresh materializes that fixture as the explicit node resolver
and verifies config, node, and real OCI sandbox DNS paths without host-resolver
or public-resolver fallback.

Tier 2 proves correctness with a small deterministic matrix. Sampling must not
turn it into an hours-long qualification. If performance evidence is needed,
keep the correctness smoke and qualification as distinct profiles and outputs.

## Tier 3: asynchronous full repository gate

`make verify-full` is the broad-change and post-merge repository gate. It is not
a default prerequisite for every pull-request merge or a command to restart
after each edit. On failure, first reproduce the named step directly, then use
the printed `--from <step>` resume point while diagnosing:

```bash
make verify-full ARGS='--include-local-storage'
make verify-full ARGS='--include-bpfnet-generate-check'
make verify-full ARGS='--include-proto-breaking'
```

Use `make verify-release` for the source and local-deployment release gate. It
includes Axrun and local-storage acceptance, but it does not produce an
environment qualification receipt.

## Tier 4: environment qualification

Digest-pinned memory, network-policy performance, capacity, soak, regional
rollout, and deployed acceptance run only for a frozen release candidate or an
explicitly scoped investigation. They must execute and collect remotely without
depending on a live developer shell.

A reduced remote smoke may provide correctness feedback, but it must have a
distinct profile and output and can never be presented as qualification or
promotion evidence. Full qualification preserves its existing sample counts,
budgets, immutable artifact identity, environment identity, and receipt checks.

The final `git status --short` must contain only intentional changes. The handoff
records the exact commands and results for the tiers that were required, rather
than claiming tiers that were skipped by scope.
