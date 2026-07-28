// Package runkernel owns durable run and environment persistence behavior in
// controld.
//
// Put run admission, cancellation, lease issuance, allocation-status
// persistence, environment storage, and reconcile bookkeeping here when the
// behavior updates authoritative Postgres-backed run state.
//
// Use params structs for the broad admission-style entrypoints that naturally
// collect many business inputs, such as environment creation or run admission.
// Keep short lifecycle methods positional when the call shape is already clear;
// objectifying every two- or three-field method makes the kernel harder to
// read, not easier.
//
// Keep request validation and API defaults out of this package, and do not move
// placement policy or node RPC request construction here. Those concerns belong
// in internal/api/*, internal/placement, and internal/nodebridge.
package runkernel
