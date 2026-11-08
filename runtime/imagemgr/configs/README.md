# Registry Configuration

The OCI daemon accepts an optional registry credentials file through `-registry_auths_path`. [registry_auths.json.example](../registry_auths.json.example) documents the flat host/repository format used by the local development target. Keep real credentials outside the checkout and supply a private file when needed. Public pulls and local Docker archive imports do not require static credentials.

The parser accepts either the flat format or Docker config JSON with an `auths` object:

```json
{
  "auths": {
    "registry.example.com": {"auth": "BASE64_USERNAME_COLON_PASSWORD"},
    "registry.example.com/team/image": {"auth": "BASE64_REPOSITORY_CREDENTIAL"}
  }
}
```

An exact host/repository entry takes precedence over a host entry. Optional request-scoped `docker_config_json` on `/oci_mount` overrides static credentials for that request. See the parser in `pkg/registryauth` and transport in `pkg/imageregistry` when changing matching or transport behavior.

From `runtime/imagemgr`, run against a fresh, owned directory:

```bash
go run ./cmd/imagemgr \
  -root /tmp/imagemgr \
  -registry_auths_path /private/imagemgr-registry-auths.json \
  -http_sock /tmp/imagemgr.sock
```

`-registry_mirror_url` selects a registry mirror; standard proxy environment variables select a forward proxy. HTTPS certificate verification remains enabled. `IMAGEMGR_INSECURE_REGISTRIES` explicitly permits HTTP for the selected hosts. The complete flag and API surface is in the [module README](../README.md).

The root target `make imagemgr-dev-run` uses the credential-free example file for the repository-owned Linux development workspace.
