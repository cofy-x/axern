# bpfnet Agent Contract

## Purpose

`network/bpfnet` is the eBPF network data-plane library used by axnoded. Use the [bpfnet README](README.md) for architecture and commands.

## Ownership Boundaries

- Bpfnet owns packet processing, attachment, pin state, SNAT collection, and diagnostics; axnoded owns Allocation lifecycle and network-resource orchestration.
- Keep it a library with one writer. Diagnostic tools may inspect state but cannot become another daemon or authority.
- Required eBPF paths fail closed. Any explicit alternative backend remains a separate operator choice, never a silent compatibility fallback.
- Keep metrics low-cardinality and generated source, loaders, and object artifacts synchronized.

## Validation

Run targeted Go tests; eBPF changes also require `make generate-check` and the Linux packet-path checks selected by `make verify-changed`.
