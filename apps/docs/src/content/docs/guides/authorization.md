---
title: Identity and namespace access
description: Understand Axern Principals, scoped roles, certificate rotation, and least-privilege managed execution.
---

Axern maps each verified client certificate to a durable Principal. Public CLI
and SDK calls enter through `gatewayd`; the control plane authorizes the
Principal for every platform or namespace operation.

Start by inspecting the selected context:

```bash
axern identity whoami
axern doctor --namespace default
```

The built-in roles are `platform_admin`, `namespace_admin`,
`namespace_editor`, and `namespace_viewer`. Namespace roles apply to exactly
one namespace. A viewer can inspect resources, an editor can also create and
execute workloads, and a namespace administrator can manage role bindings in
that namespace. Platform administrators manage Principals, credentials, and
platform-wide operations.

## Separate bootstrap administration from application access

The commands in this page require an existing platform-admin context. A
developer must not use their own namespace-editor context to create Principals
or grant roles. Operators should bootstrap one short-lived admin context,
create the application Principal and certificate, then switch back to the
least-privilege context for normal work.

## Add a namespace editor

Create the Principal, register its public certificate, and bind the role:

```bash
axern admin principal create developer \
  --display-name "Developer" \
  --kind human

axern admin credential add <principal-id> \
  --certificate developer.crt \
  --label laptop

axern admin role-binding grant \
  --principal-id <principal-id> \
  --scope namespace \
  --namespace default \
  --role namespace_editor
```

Register the matching private key and certificate as a separate local context;
the private key is never uploaded:

```bash
axern context set developer \
  --endpoint <gateway-host:port> \
  --service-url <gateway-http-url> \
  --tls-ca-cert ca.crt \
  --tls-cert developer.crt \
  --tls-key developer.key \
  --proxy-mode direct \
  --current

axern identity whoami
axern doctor --namespace default
```

Certificate issuance and CA policy remain operator-owned; the certificate
must be signed by the CA trusted by the gateway and the registered public
certificate must match `developer.key`.

Private keys remain in the user's context and are never uploaded. To rotate a
certificate, add the new public certificate first, switch the client context,
confirm `identity whoami`, and then revoke the old credential.

Managed Axrun workers use a dedicated service identity. Their namespace access
is further limited by the short-lived durable lease for the rollout work they
are currently executing; the worker certificate by itself cannot edit user
namespaces.

For the complete trust and audit model, see the repository's
[authorization architecture](https://github.com/cofy-x/axern/blob/main/docs/architecture/authorization.md).
