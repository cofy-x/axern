# Environment and Catalog

The environment catalog is curated and read-only. It is initialized from embedded declarative runtime-template JSON and ships official environment templates:

- `python311`
- `server-base`
- `coding-base`
- `desktop-base`

Embedded template metadata uses release-facing image refs. Local compose and kind truth environments keep the same template ids but override image refs with `AXERN_RUNTIME_CATALOG_PYTHON311_IMAGE`, `AXERN_RUNTIME_CATALOG_SERVER_BASE_IMAGE`, `AXERN_RUNTIME_CATALOG_CODING_BASE_IMAGE`, and `AXERN_RUNTIME_CATALOG_DESKTOP_BASE_IMAGE`, usually pointing to repo-built `:dev` images imported into the node-local image cache.

The catalog does not own agent or tool images. Callers may attach an explicit immutable image through the generic read-only `image_mounts` execution input; no separate catalog identity or lifecycle is created.

Catalog templates do not declare or select an OCI runtime implementation. Axern's execution boundary is `runsc`; it is platform implementation policy rather than workload input or a placement dimension.

## Environment Sources

Environments support two execution-source modes:

- template-backed via `template_id` / `template_version`
- image-backed via public OCI `image.ref`, resolved by `controld` to a digest

Image-backed environments can optionally reference a controld-managed registry credential secret via `image.registry_credential_id`. The referenced secret must be type `DOCKER_CONFIG_JSON`.

`resolved_spec` is the normalized immutable runtime input for both modes, so Run admission and node lifecycle paths consume one execution shape. Catalog identity, language, description, and advertised capabilities remain template metadata; image, mounts, defaults, and execution profile live only in the resolved specification. An Environment is immutable except for its deletion tombstone; changing the source creates another Environment and a new Run.

Deletion sets `deleted_at` once. Deleted Environments remain queryable for Run history, are excluded from lists by default, and cannot admit new Runs. Environment has no synthetic READY/DELETED status machine, hash identity, optimistic version, or mutable message.

Environments contain immutable workload inputs and remain independent from node implementation details.

## Execution Profile

Each template's `resolved_spec.execution_profile` describes node-side OCI execution policy, including runtime baseline capabilities, `RLIMIT_NOFILE`, capability-annotation behavior, network namespace annotation keys, and resource-field ignore annotations.

`desktop-base` is the desktop-capable runtime profile. It advertises `supports_computer_use` and sets the sandbox environment required for sandboxd's `computer_use` provider. Headless templates keep that capability unset.

`controld` owns this catalog policy as part of template resolution. Nodes consume the resolved profile instead of silently applying unrelated global defaults.

## Workload Command Defaults

Workloads may omit `config.argv`. In that case, the selected node keeps the OCI image default `ENTRYPOINT` / `CMD`. Any explicit `config.argv` overrides the image default command.

The catalog's `image_default_argv` is informational metadata for built-in images, not a control-plane bootstrap override.

## Secrets

Execution configs can project immutable controld-managed secrets into workloads through `secret_env` and `secret_files`. Secret values are encrypted at rest in Postgres and are never returned in plaintext after create.
