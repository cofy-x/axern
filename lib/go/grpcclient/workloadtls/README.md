# Workload TLS Primitives

This package implements internal transport identity and certificate operations. It does not own Node admission, enrollment tokens, renewal authorization, or Allocation liveness. Controld, gatewayd, axnoded and tunneld use these credentials; their API owners enforce admission and role authorization.

## Identity And Signing

A certificate contains exactly one canonical URI SAN: `spiffe://<cluster>/node/<node-id>` or `spiffe://<cluster>/service/<role>`. Supported service roles are controld, gatewayd, and tunneld. CN, DNS SAN, alternate URI encodings, and multiple URI identities cannot authorize workload roles.

The issuer checks the CSR signature and public-key strength, then constructs all identity, usage, and lifetime fields from the admitted identity supplied by its owner. It does not copy CSR subject names or extensions. Node leaves last 24 hours and cannot outlive the signing authority. Callers must authorize signing inside the Node admission transaction; possession of a CSR is not admission.

## Transport And Local Publication

`PublishBundle` validates the key/certificate pair, writes and syncs one mode-0600 PEM file, atomically replaces the destination, and syncs its directory. The owner must serialize enrollment and renewal and keep the directory operator-controlled.

Each new gRPC handshake reloads the identity bundle and trust roots. Invalid files fail the handshake without falling back to a cached credential. Clients perform full chain, validity, and server-usage verification followed by an exact URI identity comparison; DNS hostname matching is not the workload identity mechanism. Servers require verified client certificates and a valid workload URI, while their API interceptors must enforce the role/method and Node admission boundaries.

Reload affects new handshakes only. Transport I/O is bounded by the earliest local or peer certificate expiry; protocol owners must additionally enforce current authorization on existing RPCs and streams. An ExecutionLease remains the authority that limits Allocation liveness. Certificate rotation must never create a new Node or Allocation identity.

## Tests

Run `go test -race ./...` and `go vet ./...` from the parent grpcclient module. Tests cover CSR privilege escalation, trust-domain and Node mismatch, untrusted signers, ambiguous identity, credential reload, invalid reload, and atomic key/certificate replacement.

## Deployment Bootstrap

`Bootstrap.Ensure` creates deployment CA material and 90-day service leaves, never Node keys. `axern admin pki bootstrap` exposes it in the released CLI. A kernel-owned file lock rejects concurrent provisioning for the same directory and releases on process exit or crash. Repeated initialization preserves identities; `--renew-services` explicitly replaces service leaves while preserving CA and administrator fingerprint. Missing authority with existing credentials fails closed. Back up the private directory and never mount its signer outside controld.

Node enrollment is a separate, one-hour, single-CSR transaction owned by controld. The node persists pending key/CSR before registration, atomically publishes the certificate, and renews within eight hours of expiry. Service certificate renewal is an operator deployment action, not Node admission.
