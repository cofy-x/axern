# Principal And Namespace Authorization

Axern authenticates public API callers as durable Principals and authorizes every public RPC against a platform or namespace scope. PostgreSQL is the only authority for Principals, X.509 and SSH credentials, role bindings, and access audit events; gatewayd does not cache authorization state.

## Trust Boundary

Public CLI and SDK traffic terminates at gatewayd. Gatewayd verifies the client certificate, removes caller-supplied internal identity metadata, and forwards only the SHA-256 fingerprint of the verified leaf certificate to controld over gatewayd's dedicated mTLS connection. Controld accepts that fingerprint only when the immediate verified peer is `gatewayd`. Direct calls to public controld services are rejected.

Certificate subject names are not Principal identities. A credential record maps the exact certificate fingerprint to one active Principal and records the certificate expiry. Revocation or Principal disablement takes effect on the next RPC. Long-lived public control streams re-read the Principal and bindings every 15 seconds and close when the authority is no longer valid. Data-plane grant deadlines are independent of ExecutionLease.

WebSocket Terminal uses client mTLS on the public control TLS listener; the plaintext HTTP listener serves health only. SSH uses the SHA-256 fingerprint of the exact public-key wire encoding, registered as a Principal Credential with an explicit expiry. Neither protocol accepts gateway-wide bearer tokens or authorized-key files. Both use the private `GatewayControl` service to authorize the caller’s Credential against the Allocation namespace. Transport identity authorizes gatewayd to call that service but never replaces the caller’s business authorization.

Gateway-owned allocation target resolution, tunnel relay target resolution, and terminal resolution use the private `GatewayControl` service. Gatewayd does not call public resource services without a Principal. Tunnel peers remain authenticated by their short-lived session token at the relay, independently of control-plane Principal authentication. Peer token validation and relay event reporting live on the private `TunnelRelayControl` service, which accepts only the verified `tunneld` workload certificate.

`NodeControl` likewise accepts only the verified `axern-node` workload certificate. In the reverse direction, axnoded's routable listener requires client mTLS and authorizes by service: `controld` alone receives `NodeLifecycle`, while `gatewayd` alone receives `NodeSandbox`. The two identities cannot exercise each other's node authority. Internal workload services are therefore explicit authenticated boundaries, not unauthenticated exceptions to public authorization.

## Access Connection Lifetime

SSH and WebSocket sessions revalidate Principal, Credential, namespace authorization, active Node, and Allocation operability every 15 seconds with a 5-second check deadline. Rejection or a failed check cancels the access stream; it does not revoke ExecutionLease or terminate the Allocation. Grant expiry independently bounds the connection. The online propagation bound is 20 seconds excluding process scheduling pauses. Public gRPC data-plane calls have a finite grant deadline, normally five minutes, but do not share this periodic terminal check. Output-only grants require resource-read authority, can read bounded retained logs and sealed declared objects, and are capped by the immutable output deadline; they cannot execute processes or mutate files. Tunnel relay revalidation has the same 15-second interval and 5-second deadline and fails closed on control-plane errors; its session-token authority remains separate from Principal Credentials.

## Node-local operator authorization

The node-local operator API is a privileged administrative boundary, not an unauthenticated shortcut around gateway or control-plane authorization. Its Unix socket must never be world-writable. Deployment must restrict it to root or an explicit operator principal through ownership and mode, or enforce an equivalent authenticated peer identity before dispatch. Possession of an Allocation ID alone grants no authority.

Operator `Exec`, `ExecStream`, and `Wait` target an already admitted Allocation and reuse its normal process/runtime implementation. They do not advance Allocation desired state. Destructive node-local recovery is an explicitly named break-glass authority with terminal reporting, diagnostic, and idempotent cleanup obligations; routine cancellation and deletion remain control-plane operations.

Node-local daemons use purpose-specific machine interfaces. In particular, a tunnel component that only resolves an Allocation network namespace must not share a socket or principal whose ambient authority also permits exec, termination, cleanup, or broad diagnostics. Socket separation, service registration, ownership, and deployment identities must preserve that least-privilege boundary.

Runtime conformance uses a third, explicitly configured root-only Unix socket. It can create and inspect only unbound local Allocations and fails closed when an Allocation ID belongs to a control-plane admission record. Production does not enable this socket.

## Roles

| Role               | Scope     | Authority                                                                                            |
| :----------------- | :-------- | :--------------------------------------------------------------------------------------------------- |
| `platform_admin`   | platform  | All platform, namespace, resource, quota, and access administration                                  |
| `namespace_admin`  | namespace | Resource read/write, sandbox execution, quota read, and role binding administration in one namespace |
| `namespace_editor` | namespace | Resource read/write, sandbox execution, and quota read in one namespace                              |
| `namespace_viewer` | namespace | Resource and quota read in one namespace                                                             |

Namespace roles never imply access to another namespace. Resource-ID lookups resolve the authoritative namespace from PostgreSQL; an unauthorized lookup by opaque resource ID returns `NotFound` so the resource cannot be enumerated. List operations require or derive a namespace and return only authorized rows.

## Bootstrap And Rotation

Database migration does not create an implicit administrator. The separate `controld-access-bootstrap` entrypoint creates the first platform Principal and its X.509 credential before controld starts. Optional `-ssh-public-key` registers an SSH credential in that same transaction, expiring with the bootstrap certificate. Bootstrap is serialized and exactly idempotent: once access state exists, different identity material or revoked credentials are rejected; bootstrap never reactivates them.

After bootstrap, use the public API through gatewayd:

```bash
axern identity whoami
axern admin principal create developer --display-name "Developer" --kind human
axern admin credential add <principal-id> --certificate developer.crt --label laptop
axern admin credential add <principal-id> --ssh-public-key developer.pub --expires-at 2027-01-01T00:00:00Z --label ssh
axern admin role-binding grant \
  --principal-id <principal-id> \
  --scope namespace \
  --namespace default \
  --role namespace_editor
```

Rotate a certificate by adding the new credential, switching the client context to it, confirming `axern identity whoami`, and then revoking the old credential. A credential cannot revoke itself, a Principal cannot disable itself, and every mutation is transactionally rejected if it would remove the last active X.509 platform administrator. SSH-only access cannot administer the public gRPC API and cannot satisfy that recovery invariant.

All access mutations write `admin_audit_events` with the authenticated actor Principal ID. The authorization decision metric uses bounded action and result labels and never records Principal, credential, namespace, resource, or token values.

Namespace deletion is never an implicit access mutation. Active role bindings must be revoked first; revoked bindings remain durable authorization history after the Namespace is deleted.
