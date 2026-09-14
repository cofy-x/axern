---
title: Environments, Namespaces, and Quota
description: Reuse immutable environments, organize workloads by namespace, and inspect quota and admission signals.
---

An Environment is an immutable, reusable execution source: a resolved built-in template or OCI image reference that Runs and Sandboxes can share without re-resolving the image. Namespaces group resources, and quota bounds what each namespace may admit.

## Create and reuse an Environment

```bash
axern environment create --template-id python311
axern environment create --image-ref docker.io/library/python:3.12-slim
axern environment list
axern environment get <environment-id>
axern environment delete <environment-id>
```

An environment selects exactly one source: a deployment-provided `--template-id` (with optional `--template-version`), or `--image-ref`. Templates are resolution inputs rather than independently queryable resources. Private registries use a stored credential, referenced by ID:

```bash
axern environment create \
  --image-ref registry.example.com/team/base:1.0 \
  --registry-credential-id <secret-id>
```

Environments are immutable — a changed image or template means a new environment. Run admission freezes the normalized source and resolved runtime input on the Run. Deleting the reusable Environment physically removes it from future lookup and admission, but does not change or break an already admitted Run.

Pass the environment ID to any workload instead of resolving the source again:

```bash
axern run --environment <environment-id> -- python -c 'print("ok")'
```

The SDKs accept the same `environment_id` source when constructing a Sandbox, which avoids resolving the same source for every evaluation sample.

## Namespaces

Namespaces isolate resources and scope role bindings:

```bash
axern namespace list
axern namespace create team-a
axern namespace get team-a
axern namespace delete team-a
```

Workload commands default to the `default` namespace and accept `--namespace`. Only an inactive namespace can be deleted. Access within a namespace follows the roles in [Identity and namespace access](/guides/authorization/).

## Quota and admission

Quota bounds the CPU and memory a namespace may admit. Workloads that exceed the remaining quota are rejected at admission with a diagnostic code:

```bash
axern quota get --namespace default
axern quota events --namespace default
```

`quota get` reports the configured quota, current usage, and admission signals; `quota events` lists recent admission decisions. Platform tooling uses `quota list --pressure` to find constrained namespaces, and `quota set`/`quota unset` to change the configured limits.

For the underlying requests/limits model, admission phases, and diagnostic codes, see [Runtime and Resources](/architecture/resources/) and the repository's [resource model](https://github.com/cofy-x/axern/blob/main/docs/architecture/resource-model.md).
