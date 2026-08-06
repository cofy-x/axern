---
title: Troubleshooting
description: Diagnose contexts, local stacks, workloads, and tunnels with the doctor family and stable exit codes.
---

Axern's diagnostics are read-only doctors plus inspectable resource state.
Start from the layer that matches the symptom, then move down the stack.

## Platform connectivity and identity

```bash
axern doctor --namespace default
```

The platform doctor validates the selected context, mTLS certificate lifetime
and key permissions, gateway connectivity, namespace access, and the runtime
catalog without creating resources. Exit codes are stable for automation: `0`
healthy, `1` degraded (warnings such as an expiring certificate), `2` invalid
usage or connection configuration, `3` a required platform check failed.

When reachability alone is not enough, run the live probe — it creates a
temporary catalog-backed Environment, executes a small `runsc` Run, and cleans
up:

```bash
axern doctor --namespace default --probe
```

Use `axern identity whoami` to confirm the Principal, certificate, and
effective roles when a check reports authorization failures.

## Local Axern

```bash
axern local status
axern local doctor
axern local logs gatewayd --follow --tail 100
```

`local doctor` checks the host, Docker, ports, versions, and component health,
and prints an executable recommendation per failed check. Common causes:
conflicting ports (the local stack does not allocate alternates), Docker
proxy settings, and VPN-provided DNS that the host resolver files do not
list — set `AXERN_LOCAL_DNS_NAMESERVERS` and recreate the stack. See the
[`axern local` reference](/guides/local/) for the port table and recovery
flows.

## Workloads

A Run or Service that never becomes ready usually fails at source resolution,
admission, or readiness:

```bash
axern run get <run-id> --output json
axern service get <service-id> --output json
axern service events <service-id>
axern quota get --namespace default
```

Admission rejections carry a diagnostic code and summary; check namespace
quota before resizing a workload. Service rollouts expose `updated` versus
`outdated` replicas through `axern service replicas <service-id>`.

## Tunnels

```bash
axern tunnel doctor --session-id <session-id>
axern tunnel doctor --service-id <service-id> --local 127.0.0.1:8080
axern tunnel inspect <session-id>
axern tunnel events <session-id>
```

Tunnel doctor validates the session, binding, and (with `--local`) the local
upstream, exiting non-zero when it finds problems. See
[Reverse Tunnels](/guides/tunnels/) for session lifecycle semantics.

## Agent workspaces

```bash
axern agent doctor
```

The agent doctor diagnoses an agent profile: it validates the profile
configuration, then probes provider upstream compatibility and platform
reachability, exiting non-zero when a dependency is unhealthy. See
[Coding Agents](/guides/agent/).

JSON output is available on status and doctor commands (`--output json`) for
automation. If diagnostics pass but behavior is still wrong, collect the
command, its JSON output, and the relevant resource IDs before opening an
issue.
