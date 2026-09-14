# bpfnet Agent Contract

## Purpose

`network/bpfnet` is the reusable eBPF network dataplane library used by axnoded. Use the [bpfnet README](README.md) to route architecture, production acceptance, regression, and alerting work.

## Ownership Boundaries

- Bpfnet owns egress-NAT attach, packet processing, pin state, and diagnostics. Axnoded owns Allocation lifecycle, network resources, backend selection, rollback, and SNAT GC scheduling.
- Keep bpfnet a library. A diagnostic CLI may inspect state but must not become a second writer or daemon.
- Keep the public Go surface limited to configuration, dataplane attachment, SNAT garbage collection, and status.
- Preserve the configured pin-root contract and update operator documentation with intentional changes.
- Treat egress, reply restoration, failure, and replacement semantics as architecture contracts; change them together with the owning architecture and acceptance documents.
- The `ebpf` backend fails closed when its main TC dataplane is unavailable. Rollback requires explicitly selecting the separate `iptables` backend; compatibility modes may not hide loss of required eBPF packet paths.
- Keep metrics low-cardinality and put Allocation- or flow-specific evidence in node-local diagnostics.
- Keep generated eBPF source, loaders, and object artifacts synchronized.

## Validation

- Run targeted Go tests for pure library changes.
- For eBPF C changes, run `make generate` and `make generate-check` and validate the affected Linux packet paths.
- For axnoded integration changes, run the relevant node tests and Linux truth checks selected by `make verify-changed`.
