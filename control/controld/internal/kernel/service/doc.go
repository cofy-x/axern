// Package servicekernel owns service domain contracts and rules in controld.
//
// Put service-specific rules here when they define persistent service state or
// derive operator-facing service views from authoritative service-owned
// allocations. That includes service CRUD semantics, rollout policy,
// rollout/event diagnostics, autoscaling evaluation, and service read model
// helpers.
//
// Prefer params structs here only for operations whose business input is
// already broad enough to become error-prone as positional arguments, such as
// create/admit or resolved-allocation requests. Keep narrow scalar operations
// like get, delete, lease, or status transition methods positional so the API
// surface stays crisp instead of wrapping every call in boilerplate.
//
// Do not move request validation, RPC shaping, use-case orchestration, or app
// wiring into this package. Those belong in internal/api/*,
// internal/application/service, and internal/app. Also avoid putting node-bridge
// RPC construction or placement engine behavior here; those stay in
// internal/nodebridge and internal/placement.
package servicekernel
