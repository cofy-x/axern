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

The Linux executor owns an isolated `inet axern_egress` nftables table and a
dedicated policy-routing mark. Rules are keyed only by sandbox source IP and
use TPROXY to send conventional DNS and strict HTTP/HTTPS traffic to bounded
node-local inspectors. Explicit strict CIDR/transport/port grants return to the
ordinary bridge or bpfnet forwarding path, so egressd does not take ownership
of either backend's SNAT/DNAT state. Proxy upstream sockets carry a separate
bypass mark and never enter the workload namespace.

The DNS forwarder accepts only the same non-loopback IP nameservers verified by
axnoded while constructing the workload resolver configuration. It supports
UDP and TCP, preserves the query and DNSSEC/EDNS wire representation, rejects
denied questions or CNAME targets with `REFUSED`, and derives strict per-domain
destination authorizations from A/AAAA TTLs with a ten-minute safety cap. It
has no public resolver fallback. Strict HTTP reads one bounded request header
and checks Host; strict TLS reassembles one bounded ClientHello and checks SNI.
CONNECT, direct-IP Host, ECH, missing SNI, parser timeout/overflow, and a
domain/IP authorization mismatch fail closed. Traffic is then relayed without
TLS interception or application-body inspection.

Health reports DNS and strict self-test readiness plus the applied enforcement
revision. Axnoded derives workload capabilities from these exact facts, checks
the prepared allocation/attempt/IP/policy/revision proof immediately before OCI
start, reconciles active proofs after restart, and uses its existing fail-stop
capability-loss path if enforcement disappears. Telemetry exposes only mode,
action, protocol, result, latency, rule count, and allocation ID; query names,
Host, SNI, remote addresses, and full policy values are not dimensions.
