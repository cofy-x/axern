# Backend Configuration Examples

This directory holds example inputs for `runtime/imagemgr`.

They map directly to the daemon flags:

- `configs/oss_backend.json.example` -> `-oss_template`
- `configs/nydus_registry.json.example` -> `-nydus_template`
- `../oss_auths.json.example` -> `-oss_auths_path`
- `../registry_auths.json.example` -> `-registry_auths_path`

The `imagefsd` manager requires all four files at startup.

## What Each File Represents

- `oss_backend.json.example` Backend template used when launching `imagefsd` for OSS-style raw-image mounts
- `nydus_registry.json.example` Backend template used for Nydus registry-backed mounts
- `oss_auths.json.example` Endpoint and bucket keyed credentials used for OSS requests
- `registry_auths.json.example` Registry host and repository keyed credentials used for Nydus and registry access

These files are examples only. They document the expected shape and are also used by the repo-local dev workflow.

## Template Notes

`oss_backend.json.example` follows the Nydus `BackendConfigV2` layout with `type: "oss"`. The template provides default backend shape and can be combined with per-request OSS fields such as `endpoint`, `bucket`, `object`, `access_key_id`, and `access_key_secret`.

`nydus_registry.json.example` also follows `BackendConfigV2`, but uses `type: "registry"` for the Nydus bootstrap and blob fetch path.

## High-Value Fields

These are the fields most likely to matter when editing or reviewing the templates:

- OSS template: `endpoint`, `bucket_name`, and `object_prefix` define the default object location; request-time OSS mounts can still override endpoint, bucket, object, and credentials.
- OSS template: `proxy` controls backend HTTP proxy behavior and health checking for remote reads. Empty proxy values mean direct access.
- Registry template: `host` and `repo` define the default registry lookup target for Nydus-backed flows.
- Registry template: `proxy` controls a conventional forward proxy. Do not put
  a registry mirror in this field: Nydus v2.4 cannot attach the origin metadata
  required by Dragonfly's registry-mirror protocol.
- Registry template: `auth` and `registry_token` are backend-level auth fields inside the Nydus config shape; in the repo workflow, registry credentials are usually sourced from `registry_auths.json.example` instead.
- Registry template: `blob_url_scheme` and `blob_redirected_host` exist for registries that serve blobs from a redirected host or non-default scheme.
- Auth examples: `oss_auths.json.example` is keyed by `endpoint/bucket`, while `registry_auths.json.example` is keyed by host or host plus repository path.

## When To Edit Which File

- Change `oss_backend.json.example` when the default OSS backend transport or proxy behavior changes.
- Change `nydus_registry.json.example` when Nydus registry backend defaults or redirected blob handling changes.
- Change `oss_auths.json.example` when the example credential shape for endpoint-and-bucket matching changes.
- Change `registry_auths.json.example` when registry credential matching rules or example scopes change.

## Using The Example Files

From `runtime/imagemgr`:

```bash
go run ./cmd/imagemgr \
  -root /tmp/imagemgr \
  -imagefsd_bin /usr/local/bin/imagefsd \
  -oss_template ./configs/oss_backend.json.example \
  -nydus_template ./configs/nydus_registry.json.example \
  -oss_auths_path ./oss_auths.json.example \
  -registry_auths_path ./registry_auths.json.example \
  -http_sock /tmp/imagemgr.sock
```

For the repository-owned Linux workflow, the root target `make imagemgr-dev-run` uses these same example files.
