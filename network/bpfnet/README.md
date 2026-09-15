# bpfnet

`bpfnet` is Axern's in-process IPv4 eBPF egress-NAT library. It is embedded by `axnoded`; it is not a daemon and does not own Allocation lifecycle.

## Contract

- `axnoded` owns bridge/veth/netns resources, explicit backend selection, and SNAT garbage-collection scheduling.
- `bpfnet` owns TC attach, SNAT map reconciliation, native-route exclusions, pinned objects, and node-local diagnostics.
- TC egress translates sandbox TCP, UDP, and ICMP traffic. TC ingress restores replies.
- Sandbox ingress is reached through Allocation-scoped Tunnel/SSH sessions. `bpfnet` does not expose node-global endpoints.
- The native eBPF implementation is IPv4-only. IPv6 requires explicitly selecting the separate `iptables` egress backend.
- Attach or reconciliation failure is fail-closed; the running node never changes backend implicitly.

The public Go surface is deliberately small: `Config`, `Controller.EnsureAttached`, `Controller.Cleanup`, `Controller.CleanupStaleSNATMappings`, and `Controller.Status`.

## Layout

| Path | Purpose |
| --- | --- |
| package root | configuration, attach lifecycle, status, axnoded integration |
| `internal/dataplane` | Linux map reconciliation, TC attach, SNAT GC, status collection |
| `internal/tcprog` | eBPF C source and generated loaders/object artifacts |
| `cmd/bpfnetctl` | read-only diagnostics |
| `docs/` | architecture, production acceptance, regression, and alerting contracts |

## Diagnostics

```bash
make bpfnetctl-build
bin/bpfnetctl status --json
bin/bpfnetctl check --json
bin/bpfnetctl maps
bin/bpfnetctl dump snat_fwd_map --raw --limit 20
bin/bpfnetctl dump snat_rev_map --raw --limit 20
```

`check` requires both TC filters and all current pinned maps/programs to be present and openable.

## Code generation

The C source, generated Go loaders, and `.o` files are committed together. Regenerate through the pinned, proxy-aware Linux toolchain:

```bash
make generate
make generate-check
```

Regional mirror and proxy policy belongs to the Forge wrapper. The module script forwards standard proxy variables and supports `BPFNET_CODEGEN_GOPROXY`, `BPFNET_CODEGEN_GOSUMDB`, and `BPFNET_CODEGEN_APT_MIRROR_BASE_URL` overrides.
