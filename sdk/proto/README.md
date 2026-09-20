# Axern Shared Proto Contracts

Shared cross-module protobuf contracts live here.

Layout:

- `axern/control/environment/v1`: public immutable Environment specification and lifecycle API
- `axern/control/identity/v1`: public authenticated Principal identity API
- `axern/control/admin/v1`: platform administration, Principal, credential, and namespace authorization APIs
- `axern/control/run/v1`: public one-shot Run API, including the Environment input frozen at admission
- `axern/private/control/gateway/v1`: repo-internal gateway-to-control Allocation routing and access-grant protocol; never published in an SDK artifact
- `axern/control/tunnel/v1`: public tunnel session API for allocation-scoped reverse TCP tunnels
- `axern/control/quota/v1`: public namespace resource quota API
- `axern/private/control/node/v1`: repo-internal control-plane/node coordination API for ordered atomic node reporting with explicit finite execution-lease grants, Allocation lifecycle, allocation-access-grant replication, and TunnelSession replication. A fresh process identity plus monotonic sequence fences the complete `NodeSummary`; nested capability evidence has no parallel ordering identity. NodeEnrollment is exposed on a separate TLS listener: initial registration uses a one-time token and certificate renewal uses the exact verified Node URI. Normal NodeControl messages have no enrollment-token field. Node reports must include axnoded's aggregate `runtime_slots` contract; controld does not infer it from implementation-specific pools.
- `axern/control/common/v1`: shared control-plane value types including execution config, resource quantities, Allocation lifecycle, immutable strict or DNS-only sandbox egress policy, and workload diagnostic codes used by public workload views
- `axern/node/sandbox/v1`: gateway-exposed process, terminal, file/archive, readiness, and Computer Use operations; requests carry Allocation identity but no internal node target or access-grant credential, which gatewayd resolves and transports privately
- `axern/tunnel/v1`: tunnel relay data-plane peer stream API
- `axern/private/node/lifecycle/v1`: repo-internal control-plane-to-node allocation lifecycle API
- `axern/private/node/operator/v1`: repo-internal root-only node operator API for Allocation inspection, debug execution, wait, and audited break-glass recovery
- `axern/private/node/network/v1`: repo-internal machine-only Allocation network resolver used by `node-tunneld`

Commands:

```bash
make protos
make proto-generated-check
make -C sdk/proto lint
make -C sdk/proto breaking
make -C sdk/proto generate-go
```

The breaking check guards public/shared contracts. Repo-internal `axern/private/**` contracts may be redesigned with their consumers in the same change while Axern is in active development.

Environment deletion physically removes the reusable Environment resource. Each admitted Run carries its immutable normalized and resolved Environment snapshots, so node creation and recovery never use a deleted source row or a compatibility tombstone.

The root `protos` target regenerates committed Go stubs, Python SDK protobuf modules for non-private shared contracts, and runtime-internal axnoded protobuf outputs. `proto-generated-check` reruns generation and fails when committed generated outputs drift from source contracts. TypeScript packages copy public protobuf sources during SDK build.
