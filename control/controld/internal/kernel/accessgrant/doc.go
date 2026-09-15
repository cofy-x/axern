// Package accessgrantkernel owns short-lived allocation data-plane access
// credentials issued only to gatewayd. These grants do not authorize runtime
// liveness; Allocation execution leases are delivered by node heartbeats.
package accessgrantkernel
