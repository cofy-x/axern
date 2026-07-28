# AGENTS.md

## Purpose

This file is the local contract for `network/bpfnet`.

Read it before changing the `bpfnet` package shape, pin layout, packet paths,
fallback semantics, diagnostics, or the integration boundary with
`runtime/axnoded`.

## Start Here

Paths in this file are repo-root relative unless they are links from a nested
document.

- Read `README.md` for the short module map and document routing.
- Read `docs/architecture.md` before changing packet flow, attach/reconcile,
  SNAT lifecycle, fallback, state, or observability.
- Read `docs/production-replacement-baseline.md` before changing benchmark
  acceptance gates or interpreting whether bpfnet can replace `iptables`.
- Read `docs/production-regression-runbook.md` before running production-style
  Kubernetes bpfnet regression.
- Read `docs/production-alerting.md` before changing bpfnet readiness,
  fallback, or alert semantics.
- For axnoded integration or config-facing changes, also read
  `runtime/axnoded/docs/resource.md`,
  `runtime/axnoded/docs/configuration.md`, and
  `runtime/axnoded/docs/verification.md`.

## Hard Architecture Constraints

- `network/bpfnet` owns reusable network dataplane orchestration only.
- `runtime/axnoded` owns sandbox lifecycle, service intent, backend selection,
  rollback policy, and SNAT GC scheduling.
- Keep `bpfnet` as a library module; do not introduce a long-running daemon unless explicitly requested.
- A repo-local diagnostic CLI is allowed for read-only inspection. It must not replace `axnoded` as the writer of service intent or dataplane lifecycle.
- The public Go surface should stay small and control-plane focused: configuration, dataplane attach, service upsert/delete, and status.
- Preserve the default pin root at `/sys/fs/bpf/axern/bpfnet` unless the change intentionally updates operator-facing configuration and docs together.
- Treat the current `axnoded` target as external TCP/UDP hostPort ingress, container TCP/UDP/ICMP egress SNAT, and Linux localhost TCP hostPort only. Linux localhost UDP is out of scope unless a new design is explicitly requested.
- Treat `iptables-full-fallback` as rollback, not a successful bpfnet
  production replacement state. `localhost-tcp-iptables-compat` is acceptable
  only when TC ingress and egress remain on eBPF.
- Keep Prometheus-facing labels low-cardinality. Use node-local diagnostics for
  service, allocation, image, path, or single-flow forensic detail.

## Documentation Rules

- Keep `README.md` as the concise entrypoint; put packet-flow detail in
  `docs/architecture.md`.
- Keep `docs/production-replacement-baseline.md` focused on reusable production
  replacement gates and reference numbers.
- Keep `docs/production-regression-runbook.md` focused on repeatable command
  shapes and acceptance checks, not one-off cluster transcripts.
- Keep `docs/production-alerting.md` focused on durable alert signals. Do not
  alert on healthy close-path counters unless they correlate with risk counters,
  failures, or persistent map retention.
- Do not add environment-specific rollout logs, kubeconfigs, image tags,
  registry names, secrets, or one-off command transcripts to bpfnet docs.
- Prefer direct long-term design statements over past migration narratives.

## Validation Expectations

- For pure Go logic, run targeted `go test` in `network/bpfnet`.
- For cross-module changes with `runtime/axnoded`, also run the relevant `axnoded` tests or Linux-target compile checks.
- For eBPF C changes, regenerate committed artifacts with `make generate` and
  verify with `make generate-check`.
- If toolchain or kernel-dependent verification cannot run in the current environment, say so explicitly in the handoff.

## Required Sync Points

- If the `bpfnet` public Go API changes, update `runtime/axnoded` integration code and docs in the same change.
- If supported packet paths, attach behavior, fallback semantics, pin paths, or
  SNAT lifecycle changes, update:
  - `network/bpfnet/README.md`
  - `network/bpfnet/docs/architecture.md`
  - `runtime/axnoded/README.md`
  - `runtime/axnoded/docs/configuration.md`
  - `runtime/axnoded/docs/sample_conf.toml`
  - `.x/project-overview.md` or `.x/module-guide.md` if module ownership or routing changes
- If production acceptance gates, reference benchmark numbers, or replacement
  interpretation changes, update
  `network/bpfnet/docs/production-replacement-baseline.md`.
- If generated eBPF artifacts change, keep `internal/tcprog/bpf_nat.c`,
  generated Go loaders, and generated `.o` files together.
