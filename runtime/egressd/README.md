# egressd

`egressd` is Axern's trusted node-local sandbox egress policy daemon. It listens
on `/run/egressd/egressd.sock` and stores crash-recoverable policy lifecycle
state under `/var/lib/egressd`.

The private gRPC API supports prepare, delete, get, list, reconcile, and health.
Records are fenced by allocation ID and attempt and include the sandbox IP,
canonical policy digest, execution revision, and recovery state. A prepare is
idempotent only when all enforcement inputs match. Reconciliation retains only
records backed by an exact active-allocation proof and removes orphaned or
mismatched records.

This module establishes the lifecycle and persistence boundary. Host-side DNS,
nftables/TPROXY, HTTP Host, and TLS SNI enforcement are added by the strict
egress implementation tracked separately; until those executors are healthy,
the node capability contract remains unavailable and policy workloads must not
be admitted.

The module also owns bounded DNS message, HTTP request-header, and fragmented
TLS ClientHello inspection foundations. DNS upstream configuration accepts
only explicit IP nameservers supplied by axnoded and has no host-config or
public-resolver fallback. Its telemetry wrapper exposes only mode, action,
protocol, result, latency, rule count, and allocation ID; query names, Host,
SNI, remote addresses, and full policy values are not accepted as dimensions.
