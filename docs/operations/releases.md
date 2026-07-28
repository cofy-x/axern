# Release Operations

Axern releases one coherent version across the CLI, Helm chart, platform
images, runtime catalog images, and source metadata. `VERSION` is the canonical
version without a leading `v`; a Git tag must be exactly `v$(cat VERSION)`.

## Prepare

Update `VERSION`, package metadata, the chart version, chart `appVersion`, and
default Axern image tags together. Runtime binaries embedded in node and
development images are pinned with architecture-specific digests in
`runtime/axnoded/runtime-tools.sh`; update that single contract deliberately,
never by switching a release build to a rolling `latest` URL. Then run:

```bash
make release-check
make release-build
make open-source-check
```

The release tag must point at a commit already accepted by the normal `main`
branch checks. Releases are immutable: never move an existing tag or overwrite
a GitHub Release, OCI chart version, or final image tag.

## Publish

Push an annotated `vX.Y.Z` tag. The release workflow then:

1. verifies all version contracts;
2. builds checksummed Darwin and Linux CLI archives for amd64 and arm64;
3. builds per-architecture platform and runtime images;
4. generates SPDX and CycloneDX source SBOMs plus unified checksums;
5. installs the candidate CLI, chart, and amd64 images into a fresh kind
   cluster and executes a real sandbox Run before any final version is used;
6. publishes multi-architecture GHCR manifests and the OCI Helm chart;
7. attests and attaches the release files to a GitHub Release; and
8. repeats the fresh-kind Run against the anonymously readable published
   artifacts.

Container images carry the public source-repository label and the chart carries
matching source metadata so GHCR associates packages with this public
repository. After the first release, confirm that every image and
`charts/axern` allow anonymous reads. A release is not complete while any
package remains private.

## Verify

Wait for the `Release` workflow, including `acceptance`, to pass. Then verify
anonymous access from a clean environment:

```bash
docker buildx imagetools inspect ghcr.io/cofy-x/axern/controld:v$(cat VERSION)
helm show chart oci://ghcr.io/cofy-x/charts/axern --version "$(cat VERSION)"
```

The GitHub Release must contain four CLI archives, `checksums.txt`, SPDX and
CycloneDX SBOMs, and the matching Helm package. The quickstart and Kubernetes
commands in the root README are the public installation contract and must be
rerun when those paths change.
