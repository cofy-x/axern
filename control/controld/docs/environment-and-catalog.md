# Environment and Catalog

The runtime catalog is curated and read-only. It is initialized from embedded
declarative runtime-template JSON and ships official runtime templates:

- `python311`
- `server-base`
- `coding-base`
- `desktop-base`

Embedded template metadata uses release-facing image refs. Local compose and
kind truth environments keep the same template ids but override image refs with
`AXERN_RUNTIME_CATALOG_PYTHON311_IMAGE`,
`AXERN_RUNTIME_CATALOG_SERVER_BASE_IMAGE`,
`AXERN_RUNTIME_CATALOG_CODING_BASE_IMAGE`, and
`AXERN_RUNTIME_CATALOG_DESKTOP_BASE_IMAGE`,
usually pointing to repo-built `:dev` images imported into the node-local image
cache.

The same public catalog service exposes read-only Agent Bundles separately from
runtime templates. The embedded catalog contains `claude-code` and `codex` with
their versioned image descriptor and absolute in-bundle binary path. Deployments
may override only their image references with
`AXERN_AGENT_BUNDLE_CLAUDE_CODE_IMAGE` and
`AXERN_AGENT_BUNDLE_CODEX_IMAGE`.

Agent bundles are mounted read-only into a workspace runtime at
`/opt/axern/agents/<agent>`. They are not valid Environment templates or task
root filesystems.

Catalog templates do not declare the OCI runtime implementation. `runsc` and
`runc` are workload execution choices carried on `ExecutionConfig.runtime_class`;
when a workload omits that field, `controld` applies its default `runsc`
placement and node lifecycle policy.

## Environment Sources

Environments support two execution-source modes:

- template-backed via `template_id` / `template_version`
- image-backed via public OCI `image.ref`, resolved by `controld` to a digest

Image-backed environments can optionally reference a control-plane-managed
registry credential secret via `image.registry_credential_id`. The referenced
secret must be type `DOCKER_CONFIG_JSON`.

`resolved_template` remains the normalized runtime snapshot for both modes, so
run, service, Function worker, and node lifecycle paths consume a single
environment model. Services can update `environment_id`; image or template
changes then roll through the service replacement policy.

Environments are runtime-neutral. The same digest-pinned template or image
environment can be executed with different runtime classes by different
workloads.

## Execution Profile

Runtime templates carry an `execution_profile` that describes node-side OCI
execution policy for that template, including runtime baseline capabilities,
`RLIMIT_NOFILE`, capability-annotation behavior, network namespace annotation
keys, and resource-field ignore annotations.

`desktop-base` is the desktop-capable runtime profile. It advertises
`supports_computer_use` and sets the sandbox environment required for
sandboxd's `computer_use` provider. Headless templates keep that capability
unset.

`controld` owns this catalog policy as part of template resolution. Nodes
consume the resolved profile instead of silently applying unrelated global
defaults.

## Workload Command Defaults

Workloads may omit `config.argv`. In that case, the selected node keeps the OCI
image default `ENTRYPOINT` / `CMD`. Any explicit `config.argv` overrides the
image default command.

The catalog's `image_default_argv` is informational metadata for built-in
images, not a control-plane bootstrap override.

## Secrets

Execution configs can project immutable control-plane-managed secrets into
workloads through `secret_env` and `secret_files`. Secret values are encrypted
at rest in Postgres and are never returned in plaintext after create.
