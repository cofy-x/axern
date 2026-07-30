# Principal And Namespace Authorization

Axern authenticates public API callers as durable Principals and authorizes
every public RPC against a platform or namespace scope. PostgreSQL is the only
authority for Principals, X.509 credentials, role bindings, and access audit
events; gatewayd does not cache authorization state.

## Trust Boundary

Public CLI and SDK traffic terminates at gatewayd. Gatewayd verifies the client
certificate, removes caller-supplied internal identity metadata, and forwards
only the SHA-256 fingerprint of the verified leaf certificate to controld over
gatewayd's dedicated mTLS connection. Controld accepts that fingerprint only
when the immediate verified peer is `gatewayd`. Direct calls to public controld
services are rejected.

Certificate subject names are not Principal identities. A credential record
maps the exact certificate fingerprint to one active Principal and records the
certificate expiry. Revocation or Principal disablement takes effect on the
next RPC. Long-lived public streams re-read the Principal and bindings every 15
seconds and close when the authority is no longer valid.

The HTTP websocket terminal and SSH-compatible terminal are gateway-owned data
plane protocols rather than public control RPCs. They retain their existing
gateway bearer-token or authorized-key boundary. Their internal allocation
resolution RPC is accepted only from the verified gatewayd workload identity;
the public gRPC `NodeSandbox` path always carries and authorizes the caller's
Principal fingerprint.

Gateway-owned service routing, tunnel relay target resolution, terminal
resolution, and dashboard replica discovery use the private `GatewayControl`
service. Gatewayd does not call public resource services without a Principal.
Tunnel peers remain authenticated by their short-lived session token at the
relay, independently of control-plane Principal authentication. Peer token
validation and relay event reporting live on the private `TunnelRelayControl`
service, which accepts only the verified `tunneld` workload certificate.

`NodeControl` likewise accepts only the verified `axern-node` workload
certificate. Internal workload services are therefore explicit authenticated
boundaries, not unauthenticated exceptions to public authorization.

## Roles

| Role | Scope | Authority |
| :--- | :--- | :--- |
| `platform_admin` | platform | All platform, namespace, resource, quota, and access administration |
| `namespace_admin` | namespace | Resource read/write, sandbox execution, quota read, and role binding administration in one namespace |
| `namespace_editor` | namespace | Resource read/write, sandbox execution, and quota read in one namespace |
| `namespace_viewer` | namespace | Resource and quota read in one namespace |

Namespace roles never imply access to another namespace. Resource-ID lookups
resolve the authoritative namespace from PostgreSQL; an unauthorized lookup by
opaque resource ID returns `NotFound` so the resource cannot be enumerated.
List operations require or derive a namespace and return only authorized rows.

`rollout_executor` is an internal service role and is not grantable through the
public AccessAdmin API. A managed worker can execute public resource and sandbox
operations only while presenting its current durable rollout work lease. The
lease is stored as a hash, expires with the work claim, and is valid only in the
owning rollout namespace. The worker's long-lived certificate alone grants no
namespace resource authority.

## Bootstrap And Rotation

Database migration does not create an implicit administrator. The separate
`controld-access-bootstrap` entrypoint creates the first platform Principal,
its credential, and the managed rollout worker service Principal before
controld starts. Bootstrap is serialized and exactly idempotent: once access
state exists, different identity material is rejected.

After bootstrap, use the public API through gatewayd:

```bash
axern identity whoami
axern admin principal create developer --display-name "Developer" --kind human
axern admin credential add <principal-id> --certificate developer.crt --label laptop
axern admin role-binding grant \
  --principal-id <principal-id> \
  --scope namespace \
  --namespace default \
  --role namespace_editor
```

Rotate a certificate by adding the new credential, switching the client
context to it, confirming `axern identity whoami`, and then revoking the old
credential. A credential cannot revoke itself, a Principal cannot disable
itself, and every mutation is transactionally rejected if it would remove the
last active platform administrator.

All access mutations write `admin_audit_events` with the authenticated actor
Principal ID. The authorization decision metric uses bounded action and result
labels and never records Principal, credential, namespace, resource, or token
values.

Namespace deletion is never an implicit access mutation. Active role bindings
must be revoked first; revoked bindings remain durable authorization history
after the Namespace is deleted.
