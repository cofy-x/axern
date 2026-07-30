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
`AXERN_CLI_E2E_IMAGE_REF_RUNTIME_CLASSES`. To use a proxy, export standard
`HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` values or the focused
`VERIFY_DOCKER_*_PROXY` overrides. The shared Docker verifier converts
loopback proxy hosts for container access and includes local addresses in
`REGISTRY_NO_PROXY`; it does not probe or select a host proxy.
