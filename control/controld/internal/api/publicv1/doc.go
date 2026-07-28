// Package publicv1 is the target home for public Environment, Run, Service,
// Function, and Catalog v1 gRPC handlers.
//
// The current controlplane package remains the composition root during the
// kernel split. Handler methods should move here once their dependencies are
// expressed as narrow interfaces instead of direct access to controlplane
// internals.
package publicv1
