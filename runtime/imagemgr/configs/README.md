# Backend Configuration Examples

This directory holds example inputs for `runtime/imagemgr`.

They map directly to the daemon flags:

- `configs/nydus_registry.json.example` -> `-nydus_template`
- `../registry_auths.json.example` -> `-registry_auths_path`

The `imagefsd` manager requires both files at startup.

## What Each File Represents

- `nydus_registry.json.example` Backend template used for Nydus registry-backed mounts
- `registry_auths.json.example` Registry host and repository keyed credentials used for Nydus and registry access

These files are examples only. They document the expected shape and are also used by the repo-local dev workflow.

## Template Notes

`nydus_registry.json.example` follows `BackendConfigV2`, and uses `type: "registry"` for the Nydus bootstrap and blob fetch path.

## High-Value Fields

These are the fields most likely to matter when editing or reviewing the templates:

- Registry template: `host` and `repo` define the default registry lookup target for Nydus-backed flows.
- Registry template: `proxy` controls a conventional forward proxy. Do not put a registry mirror in this field: Nydus v2.4 cannot attach the origin metadata required by Dragonfly's registry-mirror protocol.
- Registry template: `auth` and `registry_token` are backend-level auth fields inside the Nydus config shape; in the repo workflow, registry credentials are usually sourced from `registry_auths.json.example` instead.
- Registry template: `blob_url_scheme` and `blob_redirected_host` exist for registries that serve blobs from a redirected host or non-default scheme.

## When To Edit Which File

- Change `nydus_registry.json.example` when Nydus registry backend defaults or redirected blob handling changes.
- Change `registry_auths.json.example` when registry credential matching rules or example scopes change.

## Using The Example Files

From `runtime/imagemgr`:

```bash
go run ./cmd/imagemgr \
  -root /tmp/imagemgr \
  -imagefsd_bin /usr/local/bin/imagefsd \
  -nydus_template ./configs/nydus_registry.json.example \
  -registry_auths_path ./registry_auths.json.example \
  -http_sock /tmp/imagemgr.sock
```

For the repository-owned Linux workflow, the root target `make imagemgr-dev-run` uses these same example files.
