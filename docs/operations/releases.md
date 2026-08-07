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

Confirm the public documentation still matches the release. Every English
page in `apps/docs` changed since the previous release must have its
Simplified Chinese counterpart updated in the same release; the `zh-cn`
locale is a maintained journey, not a best-effort fallback. Review
time-bounded statements in user-facing pages — currently the storage guide's
note that the stable CLI has no volume management workflow — and update or
remove any statement the release makes stale. Run `make docs-verify` before
tagging.

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

Homebrew publication uses a separate `Homebrew` workflow so a tap outage or
credential problem never requires rebuilding immutable release artifacts. Its
`homebrew-release` environment must provide `HOMEBREW_TAP_TOKEN`, a
fine-grained token with Contents write access only to
`cofy-x/homebrew-tap`. Protect that environment independently from
`sdk-release`; do not expose the tap credential to the release-tag checkout.

The release tag must point at a commit already accepted by the normal `main`
branch checks. Releases are immutable: never move an existing tag or overwrite
a GitHub Release, OCI chart version, or final image tag.

Add `docs/releases/vX.Y.Z.md` when a release needs a curated upgrade boundary
or highlights that generated commit notes cannot express. Start the file with
one H1 and a blank line; the release workflow prepends the remaining body to
GitHub's generated notes. Keep operational history out of architecture and
product documents.

Before creating tags, dispatch the `Release` workflow against the accepted
`main` commit and verify that its recorded head SHA is the intended release
commit. A manual run builds the same artifacts and architecture images,
executes the full candidate kind and SDK data-plane acceptance, proves the
source-free local experience on Linux amd64 and arm64, and validates the
Homebrew formula generated from the candidate checksums. All publication jobs
are structurally restricted to tag events. Candidate images use commit-scoped
tags so a later qualification cannot overwrite an earlier candidate. Do not
create the release tags until this candidate run succeeds.

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
6. installs the candidate CLI from release archives on Linux amd64 and arm64,
   then verifies `local up`, a foreground Run, retained data, and reset without
   using repository deployment assets;
7. generates and validates the four-platform Homebrew formula against the
   candidate release checksums;
8. installs the candidate Python wheel, npm tarball, and standalone Go module
   in clean consumers, then uses every SDK to create a `runsc` Sandbox, execute
   Python, and prove through `axern service get` that the CLI observes the same
   live resource;
9. publishes `axern-sdk` to PyPI and `@cofy-x/axern-sdk` to npm with trusted
   publishing;
10. publishes multi-architecture GHCR manifests and the OCI Helm chart;
11. attests and attaches the release files to a GitHub Release;
12. waits in one bounded readiness gate until PyPI and npm expose the exact
    candidate digests and the public Go proxy plus checksum database resolve
    the expected Go tag commit;
13. repeats the source-free local smoke, fresh-kind Run, and complete SDK
    data-plane acceptance from anonymously readable PyPI, npm, and Go module
    artifacts on Linux amd64 and arm64.

Do not query the final Go module version through `proxy.golang.org` before the
two release tags exist. A pre-release 404 can remain in public negative caches
after the tag is pushed. Preflight checks use Git refs to prove that a version
is unused; the readiness gate owns public Go module propagation after publish.

Registry publication is safely repeatable but remains immutable. On a rerun,
the workflow skips an SDK version only when every PyPI filename and SHA-256, or
the npm tarball SHA-1, exactly matches the candidate built by the workflow. An
existing version with different bytes fails the release instead of being
overwritten. A tag that starts a release is always consumed and must never be
moved. Advance the coherent version when a code or artifact fix is required.
When all immutable artifacts already match and only registry propagation or a
downstream acceptance step failed, rerun the failed jobs against the same tags.

Container images carry the public source-repository label and the chart carries
matching source metadata so GHCR associates packages with this public
repository. After the first release, confirm that every image and
`charts/axern` allow anonymous reads. A release is not complete while any
package remains private.

## Publish Homebrew

After a successful tag-triggered `Release` run, the independent `Homebrew`
workflow prepares the formula without credentials, publishes it through the
protected `homebrew-release` environment, and verifies a real installation on
macOS. Formula reconciliation is idempotent: it creates or upgrades
`Formula/axern.rb`, rejects downgrades, and rejects different content for an
already-published version.

The workflow also supports an explicit backfill for a completed release:

```bash
gh workflow run homebrew.yml --ref main -f version="$(cat VERSION)"
```

Use backfill when the workflow was introduced after a release or when tap
credentials were unavailable at release time. It consumes the immutable
GitHub Release checksums and never rebuilds CLI archives.

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
brew install cofy-x/tap/axern
axern version
```

The GitHub Release must contain four CLI archives, `checksums.txt`, SPDX and
CycloneDX SBOMs, and the matching Helm package. The quickstart and Kubernetes
commands in the root README are the public installation contract and must be
rerun when those paths change. A release is not fully distributed until the
independent Homebrew workflow succeeds or the tap is explicitly declared
unavailable in user-facing installation documentation.
