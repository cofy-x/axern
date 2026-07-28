// Package axernsdk provides the Go client surface for Axern programmable
// sandboxes.
//
// The package owns the SDK-side lifecycle for service-backed sandboxes,
// attached processes, platform file APIs, archive directory transfer, and
// tunnel sessions. Runtime behavior is delegated to Axern control, node, and
// tunnel APIs; the SDK does not implement shell fallbacks for sandbox files or
// process control.
package axernsdk
