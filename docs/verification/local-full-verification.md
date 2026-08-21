# Verification Tiers

Verification is layered by cost and evidence. A normal local edit must not run
the deployment qualification matrix or repeatedly execute destructive runtime
conformance. Run each tier on the same commit and worktree; advance only as far
as the change and delivery stage require.

## Tier 1: fast contract and unit checks

Run targeted package tests while editing, then the host-safe repository subset
before handoff:

```bash
go test ./path/to/changed/package ./path/to/adjacent/contract/package
make -C runtime/axnoded test-host
make agent-doc-check
```

When protobufs change, also run the repository proto generation and generated
checks. Tier 1 must not start real OOM or disk-fill workloads. Its normal budget
is minutes, not hours.

## Tier 2: affected local Linux integration

Use the Linux devbox or privileged Docker verification only for affected
runtime boundaries:

```bash
make -C runtime/axnoded verify-docker
```

Prefer the narrow runc, runsc, sandboxd, rootfs, cgroup, XFS, or EROFS target
when the change does not cross both runtimes. A broad runtime/rootfs change may
also run:

```bash
make local-compose-refresh-verify
make kind-refresh-verify
```

Compose DNS verification requires an explicit resolver set that is reachable
from the Node container. For example:

```bash
AXERN_VERIFY_DNS_NAMESERVERS="$REACHABLE_RESOLVER_IPS" \
  make local-compose-refresh-verify
```

`AXERN_VERIFY_DNS_NAMESERVERS` is a comma-separated list of IPv4 or IPv6
addresses. Verification deliberately does not discover host resolvers or fall
back to a public resolver: the caller must choose infrastructure appropriate
for the environment being qualified. The refresh target materializes that set
into the Compose stack and runs `local-compose-dns-doctor-smoke`, which verifies
the config, Node, and real OCI sandbox DNS layers together with redaction,
cleanup, table output, and exit-code contracts. Run the standalone smoke only
after the same resolver set has been materialized by a Compose refresh.

Tier 2 proves lifecycle and kernel integration with a small deterministic
matrix. It may exercise a single startup conformance sandbox, but it does not
run 20-sample environment qualification, maximum-concurrency stages, or the
complete image-performance matrix. Keep this tier within roughly 30–90 minutes;
split or narrow a command that approaches multi-hour duration.

## Tier 3: full repository gate

`bash ./scripts/verify-all.sh` remains a release/broad-change gate. It is not a
command to rerun from the beginning after every edit. On failure use the printed
`--from <step>` resume point after first reproducing the failing step directly.
Optional storage, BPF generation, and proto-breaking checks are selected only
when those surfaces changed:

```bash
bash ./scripts/verify-all.sh --include-local-storage
bash ./scripts/verify-all.sh --include-bpfnet-generate-check
bash ./scripts/verify-all.sh --include-proto-breaking
```

The final `git status --short` must contain only intentional changes and every
executed tier must record its exact command and result in the handoff.

Destructive environment qualification—real-OOM and quota-fill repetitions,
page-cache and dirty/writeback attribution, maximum-concurrency stages, system
reserve calibration, cloud rollout, and deployed acceptance—is intentionally
outside this repository's local verification contract. Its commands, release
identity, receipts, and environment policy belong to the deployment workspace
that owns those environments. Do not duplicate that runbook here or move those
workloads into `go test`, provider refresh, or per-allocation audit paths.
