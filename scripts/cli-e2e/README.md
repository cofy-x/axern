# Axern CLI E2E

This directory owns the product CLI end-to-end verification flow.

- `axern-cli-e2e.sh` is the stable hermetic entrypoint used by
  `make axern-cli-e2e`.
- `axern-cli-image-ref-e2e.sh` is the external image-ref smoke entrypoint used
  by `make axern-cli-image-ref-e2e`; it keeps registry/proxy coverage separate
  from the hermetic product CLI flow.
- `lib.sh` owns shared configuration, cleanup, diagnostics, and wait helpers.
- `environment.sh` starts the hermetic Postgres, storaged, controld, gatewayd,
  and node runtime environment.
- `admin_lifecycle.sh`, `catalog_namespace_quota.sh`, `quota_admission.sh`,
  `base_environment.sh`, `ssh_gateway.sh`, `service_rollout.sh`,
  `service_volume.sh`, `run.sh`, and `image_ref.sh` own scenario checks.

Keep product CLI e2e coverage as focused scenario files in this directory. Wire
hermetic product coverage from `axern-cli-e2e.sh`; keep external registry or
proxy-dependent smoke coverage in a dedicated entrypoint.

The image-ref smoke uses `docker.io/library/nginx:1.27` with `runc` and `runsc`
by default and can be pointed at another external image with
`AXERN_CLI_E2E_IMAGE_REF`. Override the runtime matrix with
`AXERN_CLI_E2E_IMAGE_REF_RUNTIME_CLASSES`. When `127.0.0.1:7890` is reachable,
the shared Docker verifier exports the runtime-facing proxy as
`http://host.docker.internal:7890` and includes local addresses in
`REGISTRY_NO_PROXY`.
