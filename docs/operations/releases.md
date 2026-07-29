# Release Operations

Axern releases one coherent version across the CLI, Helm chart, platform
images, runtime catalog images, Python, Go, and TypeScript SDKs, and source
metadata. `VERSION` is the canonical version without a leading `v`.

## Prepare

Update `VERSION`, package metadata, the chart version, chart `appVersion`, and
default Axern image tags together. Runtime binaries embedded in node and
development images are pinned with architecture-specific digests in
`runtime/axnoded/runtime-tools.sh`; update that single contract deliberately,
never by switching a release build to a rolling `latest` URL. Then run:

```bash
make release-check
make release-build
make sdk-contract-verify
make sdk-artifact-verify
make open-source-check
```

The GitHub `sdk-release` environment is the deployment boundary for SDK
registries. PyPI trusts the `Release` workflow for `axern-sdk` and npm trusts
the same workflow for `@cofy-x/axern-sdk`; both use GitHub OIDC and must not use
long-lived publish tokens. Configure the npm trusted publisher for repository
`cofy-x/axern`, workflow `release.yml`, environment `sdk-release`, and allow
`npm publish`. The npm organization and initial public package must exist before
its trusted publisher can be registered. Bootstrap a validated prerelease
(for example `X.Y.Z-bootstrap.0` under the `next` dist-tag) interactively with
2FA from a temporary clean export; do not consume the final version. Then
configure trust and remove the bootstrap credential before creating the release
tags. npm generates provenance automatically for subsequent public OIDC
publishes.

The release tag must point at a commit already accepted by the normal `main`
branch checks. Releases are immutable: never move an existing tag or overwrite
a GitHub Release, OCI chart version, or final image tag.

## Publish

Create two annotated tags on the same accepted commit and push them atomically:

```bash
version="$(cat VERSION)"
git tag -a "v${version}" -m "Axern v${version}"
git tag -a "sdk/go/v${version}" -m "Axern Go SDK v${version}"
git push --atomic origin "v${version}" "sdk/go/v${version}"
```

The directory-prefixed Go tag is required by Go's multi-module repository
versioning rules. The release workflow rejects a missing, lightweight, or
mismatched Go SDK tag. The workflow then:

1. verifies all version contracts;
2. builds checksummed Darwin and Linux CLI archives for amd64 and arm64;
3. builds per-architecture platform and runtime images;
4. generates SPDX and CycloneDX source SBOMs plus unified checksums;
5. installs the candidate CLI, chart, and amd64 images into a fresh kind
   cluster and executes a real sandbox Run before any final version is used;
6. installs the candidate Python wheel, npm tarball, and standalone Go module
   in clean consumers, then uses every SDK to create a `runsc` Sandbox, execute
   Python, and prove through `axern service get` that the CLI observes the same
   live resource;
7. publishes `axern-sdk` to PyPI and `@cofy-x/axern-sdk` to npm with trusted
   publishing;
8. publishes multi-architecture GHCR manifests and the OCI Helm chart;
9. attests and attaches the release files to a GitHub Release; and
10. repeats the fresh-kind Run and the complete SDK data-plane acceptance from
    anonymously readable PyPI, npm, and Go module artifacts.

Registry publication is safely repeatable but remains immutable. On a rerun,
the workflow skips an SDK version only when every PyPI filename and SHA-256, or
the npm tarball SHA-1, exactly matches the candidate built by the workflow. An
existing version with different bytes fails the release instead of being
overwritten. A tag that starts a release is consumed even when publication
fails: fix the root cause, advance the coherent version, and create new tags.
Never move or reuse the failed tags.

Container images carry the public source-repository label and the chart carries
matching source metadata so GHCR associates packages with this public
repository. After the first release, confirm that every image and
`charts/axern` allow anonymous reads. A release is not complete while any
package remains private.

## Verify

Wait for the `Release` workflow, including both candidate and published SDK
data-plane acceptance, to pass. Then verify anonymous access from a clean
environment:

```bash
docker buildx imagetools inspect ghcr.io/cofy-x/axern/controld:v$(cat VERSION)
helm show chart oci://ghcr.io/cofy-x/charts/axern --version "$(cat VERSION)"
uv run --no-project --with "axern-sdk==$(cat VERSION)" -- python -c 'import axern_sdk; print(axern_sdk.__version__)'
npm view "@cofy-x/axern-sdk@$(cat VERSION)" version
go list -m "github.com/cofy-x/axern/sdk/go@v$(cat VERSION)"
```

The GitHub Release must contain four CLI archives, `checksums.txt`, SPDX and
CycloneDX SBOMs, and the matching Helm package. The quickstart and Kubernetes
commands in the root README are the public installation contract and must be
rerun when those paths change.
