# Sandbox Network Policy

Axern models sandbox egress as an immutable part of `ExecutionConfig.network`. The public protobuf contract and every SDK expose two mutually exclusive policies with intentionally different security claims.

## Policy modes

`strict` is a fail-closed, default-deny boundary. Domain rules authorize only HTTP and HTTPS on TCP ports 80 and 443. Enforcement must combine controlled DNS, the destination addresses learned within the DNS TTL, and the HTTP `Host` or TLS SNI name. A matching address alone is not authorization. Direct IP, alternate DNS, DoT, QUIC, ECH, missing SNI, and HTTP `CONNECT` remain denied unless a separate CIDR rule explicitly authorizes the applicable non-HTTP protocol and port.

CIDR rules name an IPv4 or IPv6 prefix, TCP or UDP, and one or more inclusive port ranges. Loopback remains sandbox-internal. Rules cannot authorize loopback, link-local, multicast, unspecified, or cloud metadata destinations; the hard deny takes precedence even when a broader user CIDR contains one of those ranges. RFC1918 and IPv6 ULA destinations require an explicit CIDR rule.

`dns_deny` is a DNS-only convenience policy. Matching conventional UDP/TCP DNS queries, including a matching name in a CNAME chain, receive `REFUSED`. Other traffic is unchanged. In particular, direct IP, DoH, DoT, cached addresses, and application-owned resolvers are outside this policy. Product surfaces must not describe it as strict egress enforcement.

Omitting egress policy leaves networking unrestricted. Host networking conflicts with every policy and is rejected at API creation. A strict policy with no allow rules canonicalizes to isolated networking: the Allocation receives a fresh OCI network namespace without a host interface, gateway, or route. This blocks direct-IP traffic as well as DNS and HTTP/HTTPS; an isolated Allocation also has no reachable sandbox port for Tunnel access. The absence of a connected interface is checked before runtime activation. It does not depend on egressd's source-IP firewall rules.

## Canonical rules

Domain names are trimmed, lowercased, stripped of one trailing dot, and converted to IDNA A-label form. A wildcard is allowed only as the leading `*.`; it matches subdomains at any depth but not the apex. URLs, paths, ports, IP literals, embedded wildcards, invalid labels, labels longer than 63 bytes, and names longer than 253 normalized bytes are rejected.

CIDRs are masked to their canonical prefix. Protocol is explicit, and every port range satisfies `1 <= start <= end <= 65535`. Duplicate domain rules are removed. CIDR rules with the same prefix and protocol are combined, and overlapping or adjacent port ranges are merged. A policy contains at most 256 normalized domain plus CIDR rules.

## Admission and rollout boundary

The shared node-capability definition registry derives `DNS_POLICY_ENFORCEMENT` for `dns_deny` and `STRICT_EGRESS_ENFORCEMENT` for a strict policy with allow rules that requires egressd. Both capabilities use `FAIL_STOP`. Controld includes the derived key in placement and durable admission evidence. Axnoded must independently derive the same requirement before side effects and verify it again before the user process starts. Isolated deny-all instead relies on the runtime's private, unconnected OCI network namespace and does not request an egressd capability or policy record.

Nodes without egressd publish the corresponding self-test facts as `UNAVAILABLE/DISABLED`; policy workloads therefore remain unschedulable rather than running without enforcement. Changes to this contract deploy matching control-plane and node binaries together.

Policies are immutable for a Run. Changing policy requires a new Run; there is no live policy mutation API.
