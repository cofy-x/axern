# Gateway Agent Contract

## Purpose

`gateway/gatewayd` is Axern's external control and Allocation-bound data-plane gateway. Read the [Gateway README](README.md) for endpoints, configuration, and package routing.

## Ownership Boundaries

- `controld` owns placement, lifecycle, authorization state, and durable leases. Gatewayd proxies public control APIs and resolves explicit Allocation targets before forwarding terminal, SSH, Tunnel, artifact, process, or file traffic.
- Preserve `api -> application -> kernel <- adapters`; `internal/app` is the only composition root.
- Keep protocol and transport concerns in `internal/api`, use-case orchestration in `internal/application`, narrow contracts in `internal/kernel`, and external gRPC clients in `internal/adapters`.
- Do not place behavioral decisions in app wiring or hide route, lease, cache, or retry ownership in generic utilities.
- Use the dedicated gateway mTLS identity for control-plane calls; never reuse an external client identity internally.
- Every data-plane path must honor Allocation identity and attempt-scoped authorization from the [Stable Domain Model](../../docs/product/domain-model.md).

## Validation

- Run `go test ./...` and `go vet ./...` from `gateway/gatewayd`, then `make gatewayd-check-architecture` from the repository root.
- Run the relevant local Compose or SDK smoke selected by `make verify-changed` for integration-sensitive routing, terminal, SSH, Tunnel, or artifact changes.
