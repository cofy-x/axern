# Axern Shared Proto Contracts

Shared cross-module protobuf contracts live here.

Layout:

- `axern/control/catalog/v1`: public control-plane runtime catalog API
- `axern/control/environment/v1`: public immutable environment API
- `axern/control/run/v1`: public one-shot run API
- `axern/control/gateway/v1`: public gateway route and terminal target resolution API
- `axern/control/function/v1`: public serverless function API for named handler
  deployment, immutable revisions, warm-pool status, invocation records, and
  function events
- `axern/control/tunnel/v1`: public tunnel session API for allocation-scoped
  reverse TCP tunnels
- `axern/control/service/v1`: public static replica service API
- `axern/control/storage/v1`: public Storage V1 API for volume classes, claims, and workload mount intent
- `axern/control/quota/v1`: public namespace resource quota API
- `axern/control/node/v1`: shared control-plane/node coordination API for node
  reporting, allocation status, execution lease replication, and tunnel session
  replication. Node reports must include axnoded's aggregate `runtime_slots`
  contract; controld does not infer it from implementation-specific pools.
- `axern/control/common/v1`: shared control-plane value types including
  execution config, resource quantities, allocation status, internal execution
  leases, and workload diagnostic codes used by public workload views
- `axern/node/sandbox/v1`: gateway-exposed sandbox execution and allocation
  HTTP proxy API; gatewayd resolves allocations and forwards to internal nodes
- `axern/tunnel/v1`: tunnel relay data-plane peer stream API
- `axern/private/node/lifecycle/v1`: repo-internal control-plane-to-node allocation lifecycle API
- `axern/private/node/operator/v1`: repo-internal local node operator API
- `axern/private/storage/v1`: repo-internal storage coordination API for resolving requirements, reserving bindings, carrying resolved node volume specs through node lifecycle, reporting publish/release observations back to `storaged`, and exposing binding health summaries
- `axern/private/runtime/volume/v1`: repo-internal axnoded-to-volumed API for publishing, unpublishing, listing, and reconciling resolved node volume specs into node-local volume records

Commands:

```bash
make protos
make proto-generated-check
make -C sdk/proto lint
make -C sdk/proto breaking
make -C sdk/proto generate-go
```

The breaking check guards public/shared contracts. Repo-internal
`axern/private/**` contracts may be redesigned with their consumers in the same
change while Axern is in active development.

The root `protos` target regenerates committed Go stubs, Python SDK protobuf
modules for non-private shared contracts, and runtime-internal axnoded protobuf
outputs. `proto-generated-check` reruns generation and fails when committed
generated outputs drift from source contracts. TypeScript packages copy public
protobuf sources during SDK build.
