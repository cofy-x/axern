# Gateway Agent Contract

## Purpose

`gateway/gatewayd` is Axern's external control and Allocation-bound data-plane gateway. Read the [Gateway README](README.md) for endpoints, configuration, and package routing.

## Ownership Boundaries

- `controld` owns placement, lifecycle, execution authority, and durable allocation access grants. Gatewayd proxies public control APIs and resolves explicit Allocation targets before forwarding process, file, archive, terminal, SSH, or Tunnel traffic.
- Preserve `api -> application -> kernel <- adapters`; `internal/app` is the only composition root.
- Keep protocol and transport concerns in `internal/api`, use-case orchestration in `internal/application`, narrow contracts in `internal/kernel`, and external gRPC clients in `internal/adapters`.
- Do not place behavioral decisions in app wiring or hide route, access-grant, cache, or retry ownership in generic utilities.
- Use the dedicated gateway mTLS identity for control-plane calls; never reuse an external client identity internally.
- Use that same dedicated `gatewayd` workload certificate for the node data plane. Axnoded authorizes it only for `NodeSandbox`; gatewayd must never acquire `NodeLifecycle` or node-operator authority.
- Every data-plane path must honor exact Allocation identity and allocation-scoped authorization from the [Stable Domain Model](../../docs/product/domain-model.md).
- SSH public keys and Terminal client certificates are Principal Credentials, authorized by controld for the target Allocation namespace. Keep Terminal on the client-mTLS control listener and HTTP health separate. Never restore gateway-wide bearer tokens or authorized-key files. Access loss closes the stream without changing Allocation execution authority.

- Verify the resolved exact Node URI on outbound connections and include Node ID in connection-cache keys. Never use CN or a shared Node DNS alias for authorization.

## Validation

- Run `go test ./...` and `go vet ./...` from `gateway/gatewayd`, then `make gatewayd-check-architecture` from the repository root.
- Run the relevant local Compose or SDK smoke selected by `make verify-changed` for integration-sensitive routing, process, file, archive, terminal, SSH, or Tunnel changes.
