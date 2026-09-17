# Gateway Agent Contract

## Purpose

`gateway/gatewayd` is the external control and Allocation-scoped data-plane gateway. Use the [Gateway README](README.md) for endpoints and commands.

## Task Routes

| Task | Required local context |
| --- | --- |
| Authentication, grants, revocation, or namespace checks | [Authorization](../../docs/architecture/authorization.md) |
| SSH, Terminal protocol, cancellation, or connection limits | [SSH Terminal](README.md#ssh-terminal), [Terminal Protocol](README.md#terminal-protocol), [Limits](README.md#limits) |
| Allocation target resolution or Tunnel forwarding | [Runtime Architecture](../../docs/architecture/runtime-architecture.md#external-and-internal-flows), [TunnelSession](../../docs/product/domain-model.md#tunnelsession) |

## Ownership Boundaries

- Controld owns placement, lifecycle, and durable access grants; gatewayd only authenticates, resolves, and forwards public operations.
- Every data-plane path must resolve one exact Allocation and purpose-scoped authority. Never trust a client-supplied Node or runtime target.
- Internal calls use gatewayd's least-privilege workload identity, never an external Principal credential; gatewayd must not acquire lifecycle or operator authority.
- SSH and Terminal use the shared Principal and Namespace authorization model. Access loss closes the connection without redefining Allocation execution authority.
- Preserve the `api -> application -> kernel <- adapters` dependency direction and keep routing, authorization, cache, and retry ownership explicit.

## Validation

Run `go test ./...` and `go vet ./...` in this module, `make gatewayd-check-architecture`, and the integration checks selected by `make verify-changed`.
