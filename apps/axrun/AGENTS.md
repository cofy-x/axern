# Axrun Subtree Contract

## Boundary

`apps/axrun` is not an Axern product surface and does not define public Axern APIs, domain objects, release requirements, or documentation. New runner, benchmark, agent, verifier, trajectory, dataset, and training behavior belongs in an external project that consumes a released Axern SDK.

- Do not extend this subtree with new product capabilities or use it to justify changes to Axern's public model.
- Do not import control-plane, gateway, runtime, database, node-lifecycle, or implementation-only protobuf packages.
- Any necessary maintenance must preserve the platform [Stable Domain Model](../../docs/product/domain-model.md) and remain removable without changing Axern contracts.
- Security or build fixes are explicit exceptions to the frozen-source check. They must stay local to this subtree unless the same defect is independently present in an owned Axern component.

## Validation

For a necessary local maintenance change, run `go test ./apps/axrun/...`, `go vet ./apps/axrun/...`, and `test -z "$(gofmt -l apps/axrun)"`. Do not treat Axrun-specific validation as Axern release qualification.
