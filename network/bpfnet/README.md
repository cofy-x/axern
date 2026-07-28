# bpfnet

`bpfnet` is Axern's in-repo Linux eBPF NAT dataplane library. It is embedded by
`runtime/axnoded`; it is not a standalone daemon.

## Role

- Library-only dataplane control plane.
- Default bpffs pin root: `/sys/fs/bpf/axern/bpfnet`.
- Axnoded integration point: `plugin.network.nat_backend = "ebpf"`.
- Main packet paths: external TCP/UDP hostPort ingress, sandbox TCP/UDP/ICMP
  egress SNAT, TCP localhost hostPort compatibility.
- Scope is IPv4. Linux localhost UDP and IPv6 are outside the supported design.

## Ownership Contract

`runtime/axnoded` owns sandbox lifecycle, bridge/veth/netns resources, service
hostPort intent, backend selection, rollback policy, and SNAT GC scheduling.

`bpfnet` owns dataplane attach and reconciliation, service-map programming,
pinned maps and programs, TC ingress/egress links, localhost TCP cgroup links,
status collection, and fallback decisions derived from persisted dataplane
state.

`bpfnetctl` is read-only diagnostics. It must not become the writer of service
intent, attach lifecycle, cleanup, or rollback policy.

## Supported Paths

| Path | Program |
| --- | --- |
| External TCP/UDP hostPort DNAT to sandbox target | TC ingress |
| Service reply source restoration to node hostPort | TC egress |
| Sandbox TCP/UDP/ICMP egress SNAT | TC egress |
| Sandbox egress reply restoration | TC ingress |
| Host-local TCP hostPort compatibility | cgroup `connect4`, `getpeername4`, `sock_release` |
| Native-routing CIDR skip | TC egress |

If the localhost TCP cgroup path is unavailable, axnoded may use
`iptables`-based localhost TCP compatibility while TC ingress and egress remain
on eBPF. Full `iptables` fallback is a rollback state, not a successful bpfnet
replacement state.

## Public Go Surface

- `Config`
- `Controller`
  - `EnsureAttached`
  - `Cleanup`
  - `UpsertService`
  - `DeleteService`
  - `Status`
  - `NeedsSNATFallback`
  - `NeedsFullDNATFallback`
  - `NeedsLocalhostCompat`

## Layout

| Path | Purpose |
| --- | --- |
| package root | public API, persisted state, fallback decisions, axnoded-facing integration |
| `internal/dataplane` | Linux object reconciliation, map sync, TC attach, localhost cgroup attach, status collection |
| `internal/tcprog` | eBPF C source, generated loaders, committed `.o` artifacts |
| `cmd/bpfnetctl` | read-only node-local diagnostic CLI |
| `docs/architecture.md` | long-term ownership, packet-flow, state, fallback, and observability design |
| `docs/production-replacement-baseline.md` | production replacement benchmark baseline and acceptance gates |
| `docs/production-regression-runbook.md` | reusable Kubernetes production-regression command matrix |
| `docs/production-alerting.md` | production alert signals and PromQL policy |

## Documentation Route

- Start with [Architecture](docs/architecture.md) for ownership boundaries,
  packet flows, SNAT lifecycle, fallback semantics, and observability.
- Use [Production Replacement Baseline](docs/production-replacement-baseline.md)
  to decide whether bpfnet remains production-comparable to `iptables`.
- Use [Production Regression Runbook](docs/production-regression-runbook.md) to
  run the repeatable Kubernetes benchmark and rollout validation matrix.
- Use [Production Alerting](docs/production-alerting.md) for the minimal alert
  signals that distinguish rollback states from healthy close-path churn.
- Use [axnoded Verification](../../runtime/axnoded/docs/verification.md) and
  [axnoded Runtime Scripts](../../runtime/axnoded/scripts/README.md) for the
  benchmark and profile command matrix.

Keep environment-specific rollout logs, kubeconfigs, image tags, registries,
and one-off command transcripts out of these docs.

## Diagnostics

The node runtime image and axnoded verification image include
`/usr/local/bin/bpfnetctl`.

Build locally:

```bash
make bpfnetctl-build
make -C network/bpfnet build-cli
```

Common inspections:

```bash
bin/bpfnetctl status --json
bin/bpfnetctl check --json
bin/bpfnetctl maps
bin/bpfnetctl dump service_map --limit 20
bin/bpfnetctl dump rev_nat_map --raw --limit 20
```

Use `--pin-path` for a non-default bpffs pin root. Use `--state-path` when the
controller JSON state lives outside the default path. `check` exits non-zero
when required maps, programs, links, TC filters, or localhost attachments are
missing.

## Code Generation

`internal/tcprog/bpf_nat.c`, generated `.go`, and generated `.o` files are
tracked together. Normal `go build` and `go test` use the committed generated
artifacts and do not require a local `clang` or `llvm` toolchain.

Authoritative regeneration:

```bash
make generate
make generate-check
```

`make generate` runs `bpf2go` inside the pinned Linux Docker toolchain owned by
this module. Existing `axern-bpfnet-codegen:latest` images are reused by
default; set `BPFNET_CODEGEN_REBUILD=1` to rebuild the toolchain image.

Go module and build caches are persisted under
`${XDG_CACHE_HOME:-$HOME/.cache}/axern/bpfnet-codegen`; override with
`BPFNET_CODEGEN_CACHE_DIR` when needed. The codegen container sets `GOWORK=off`
so generation stays scoped to this module. `BPFNET_CODEGEN_GOPROXY` defaults to
`https://proxy.golang.org,direct` and `BPFNET_CODEGEN_GOSUMDB` defaults to
`sum.golang.org`. Override either variable explicitly when a development
network requires a regional proxy or a private checksum policy.
