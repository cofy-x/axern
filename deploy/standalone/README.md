# Native Linux Runtime Payload

This directory owns the native Linux installation assets and operating instructions. The runtime archive contains Axern services, helpers, a pinned gVisor runtime and an experimental systemd installer. See [Native Installation](INSTALL.md) for prerequisites, configuration, startup, shutdown and identity renewal.

The installation uses the existing `Environment -> Run -> Allocation` lifecycle, PostgreSQL authority and gVisor/runsc execution boundary. It requires a Linux host. It does not provision Lima or manage `axern dev` contexts. Optional Workspace budgets require the explicit XFS project-quota profile in the installation guide.

Use the opt-in [startup and idle measurement](MEASUREMENT.md) harness to record the installed candidate's startup phases, first workload output and cgroup/process resource footprint on an exclusive disposable host.

## Build

Build natively on Ubuntu 24.04, on amd64 or arm64, from a clean committed checkout. Cross-compilation and other build distributions are rejected by the initial profile. Use the repository's Go version with locked dependencies. Generated protobuf prerequisites follow the existing repository bootstrap and module build instructions. The native payload includes OCI image management.

Obtain the full `gvisor.tar.bz2` archive selected by [gvisor.lock](../../runtime/axnoded/gvisor.lock). The upstream URL is `https://storage.googleapis.com/gvisor/releases/release/<AXERN_GVISOR_RELEASE>/<x86_64-or-aarch64>/gvisor.tar.bz2`. This is a build input, not a delivered payload. The builder verifies its architecture-specific SHA-512, then selects only `runsc` and the four helpers in [gvisor-selected.lock.json](../../runtime/axnoded/gvisor-selected.lock.json). Each selected file must match its locked SHA-256, size, mode and ELF architecture before packaging; the builder does not execute upstream binaries.

```bash
make native-runtime-check
make native-runtime-build GVISOR_ARCHIVE=/absolute/path/to/gvisor.tar.bz2
```

The builder compiles the Go service entrypoints, checks the generated installer unit arguments against those binaries' help interfaces without starting services, uses temporary staging, and publishes to `dist/native-runtime/axern-runtime_<version>_linux_<arch>.tar.gz` only after verification. It refuses to replace an existing output and does not remove another build's artifacts. To compare multiple builds, use a new output directory:

```bash
python3 scripts/release/native_runtime.py build \
  --gvisor-archive /absolute/path/to/gvisor.tar.bz2 \
  --output-dir /absolute/path/to/new-candidate-directory
```

The final JSON receipt reports the archive path and SHA-256. Build output goes to stderr except for compiler/tool output. This receipt records the candidate; it is not a signed provenance attestation. A clean source revision is checked before and after building, and executable headers are checked for the declared architecture. Packaging cannot establish runtime correctness or reproduce the identity of dependencies outside the locked source and supplied runtime archive.

## Payload Contract

`manifest.json` is the first archive member. Its `axern.native-runtime.v3` schema records the version, source commit, Linux architecture, candidate build profile/libc, gVisor source/release/input-archive digest and the exact selected file inventory with sizes, SHA-256 hashes and modes. The bundled selected-member lock binds the direct-runsc profile to those bytes. Entries use deterministic order, normalized ownership/modes and source-commit timestamps; gzip omits the output filename and wall-clock timestamp. This makes packaging the same staged bytes reproducible, without claiming every compiler produces reproducible binaries.

| Payload | Purpose |
| --- | --- |
| `bin/` | CLI, control with scheduled retention, migration/access-bootstrap, gateway, node, OCI image manager, egress, tunnel and diagnostic binaries |
| `libexec/axnoded/` | Sandbox supervisor, egress/DNS probes and sandbox tunnel agent |
| `libexec/axern/` | Native installer, selected-runtime and payload verification/staging tools, and opt-in qualification runners |
| `runtime/gvisor/runsc` and `runtime/gvisor/gvisor-bin/` | Direct runsc with `gvisor_sentry`, `gvisor-sentry-prewarmer`, `gvisor-proc-umounter` and `runsc-fd-parking`, preserving their required relative layout |
| `share/axern/gvisor.lock` and `share/axern/gvisor-selected.lock.json` | Upstream provenance and architecture-specific selected-member integrity |
| `share/axern/runtime-selftest/memory-hog` and `share/axern/erofs-root/` | Axern-owned capability fixture inputs for installer assembly |
| `share/axern/registry_auths.json` | Empty OCI registry credential map |
| `requirements.json`, `LICENSE`, `NOTICE` | Candidate host requirements and Axern license notices |

PostgreSQL, Python, system libraries, Linux utilities and BusyBox are host prerequisites, not bundled executables. The installer assembles the capability rootfs using host `busybox-static` and the bundled memory-hog, and generates the EROFS fixture from its source directory. This avoids copying arbitrary host binaries into the release payload. Imagemgr owns OCI extraction and immutable mount leases.

The payload contains no node identity, TLS/private keys, database, registry credentials or mutable workspace. It contains neither the original gVisor archive nor containerd shim, checkpoint-gofer or metric-server executables. Axern uses the direct runsc lifecycle; upstream checkpoint and standalone metric-server commands are outside this selected profile. The fixed inventory rejects additional files rather than recursively archiving a developer's directory. The installer generates host-specific configuration and systemd units; staging files alone does not start services.

## Verify Without Installation

Use an expected SHA-256 from a trusted build receipt or future authenticated release metadata. Computing a checksum from an unknown download and passing that same checksum proves no origin.

```bash
make native-runtime-verify \
  RUNTIME_ARCHIVE=/absolute/path/to/axern-runtime_VERSION_linux_amd64.tar.gz \
  RUNTIME_SHA256=EXPECTED_SHA256_FROM_TRUSTED_RECEIPT
```

The verifier reads the payload without extraction or execution. It checks the outer digest, bounded manifest, closed inventory, per-file size/mode/hash and the selected gVisor members against their bundled lock and ELF architecture. Duplicate payload names, unexpected files, links, special files, credential-bearing registry templates and missing members fail. Passing verifies payload structure and integrity; it does not authenticate the publisher, inspect dynamic library compatibility, prove sandbox isolation or replace a dependency audit of the actual delivered binaries.

## Verification And Support Boundary

`make native-runtime-check` exercises corruption, architecture checks, concurrent staging, configuration validation, generated systemd units and data-identity protection. Its archive tests use synthetic ELF headers; it does not download gVisor, compile services or run a sandbox. The check is part of `make release-check`. Real native builds, service operation and sandbox enforcement require the separate [fresh-archive qualification](QUALIFICATION.md).

This candidate is deliberately outside `make release-build` and automated publication. Extending the release channel requires a payload-aware dependency/license inventory, required third-party notices, provenance/checksum publication, native architecture builds and qualified installation tests. The source-tree SBOM alone is not a claim that the complete binary/runtime archive has passed the [dependency license policy](../../docs/legal/dependency-licenses.md).

To prepare delivery evidence, scan the verified archive with the matching source's inventory tool and a trusted Syft executable.

```bash
python3 scripts/release/native_delivery_inventory.py /absolute/path/to/runtime.tar.gz \
  --sha256 EXPECTED_SHA256_FROM_TRUSTED_RECEIPT \
  --output-dir /absolute/path/to/new-inventory-directory \
  --syft /absolute/path/to/syft
```

The tool stages the verified payload without running its binaries and emits SPDX and CycloneDX sidecars. Its receipt binds both sidecar digests to the archive digest, source and architecture, and counts SPDX license declarations requiring review.

Collect the original notice texts for the Go dependencies in that inventory. Have the Go toolchains named in the inventory installed and keep the corresponding module ZIPs in the Go module cache. Download a module the collector identifies as missing with `GOWORK=off go mod download MODULE@VERSION`. Fetch the tag from the official gVisor repository using the tag name in the matching Axern source's `runtime/axnoded/gvisor.lock`.

```bash
GVISOR_TAG=release-TAG_FROM_GVISOR_LOCK
git init /absolute/path/to/gvisor-source
git -C /absolute/path/to/gvisor-source fetch --depth=1 https://github.com/google/gvisor.git "refs/tags/$GVISOR_TAG"
python3 scripts/release/native_delivery_notices.py /absolute/path/to/runtime.tar.gz \
  --sha256 EXPECTED_SHA256_FROM_TRUSTED_RECEIPT \
  --inventory-dir /absolute/path/to/new-inventory-directory \
  --gvisor-source /absolute/path/to/gvisor-source \
  --module-cache "$(go env GOMODCACHE)" \
  --output-dir /absolute/path/to/new-notice-directory
```

The collector checks the archive and both inventory digests, verifies the gVisor tag against the pinned commit, and recomputes the Go module ZIP content checksums against the relevant Axern and gVisor source locks. It collects notice files throughout the Axern and gVisor source trees, including the nested gVisor package licenses. It saves the original source and toolchain notice texts with a receipt identifying the archive, binary locations and per-file hashes. The SPDX declarations retain their original scanner values. Publication still requires review of the notice texts and any compiled inputs outside the Go inventory, including the embedded BPF object, plus signed provenance and installation qualification for the architecture being published.

Dispatch the manual native workflow on the default branch. Set `candidate_commit` to a complete SHA from that branch to select an earlier build. Omitting it uses the dispatch commit. Leave `qualify_arm64` off to build on Ubuntu 24.04 amd64 and arm64 and produce signed provenance. A separate signer verifies the selected source, architecture and inventory digests, then signs the archive digest, SHA256SUMS, receipt, SPDX and CycloneDX. Download the matching `native-evidence-linux-ARCH` artifact. Each archive stays on the machine that builds it. [Native provenance workflow](../../.github/workflows/native-provenance.yml)

Set `qualify_arm64` to build for ARM64 and run the native [acceptance matrix](QUALIFICATION.md) on a fresh GitHub-hosted `ubuntu-24.04-arm` runner. This mode leaves signing for a separate dispatch. The qualification job rebuilds the selected commit using the same Go and gVisor inputs as the ARM build. It verifies the local archive against the original build receipt before installing it. Review the `native-qualification-linux-arm64` artifact for the runner architecture and kernel version, verified archive identity, any typed startup capability diagnostics, and the eight named case results. A pass requires a report from the current boot, all eight cases and their cleanup, and a successful exit from the guest wrapper. Daily pull requests continue to use local verification.

To verify a separately obtained archive, choose the reviewed workflow commit and the expected archive source. Both are the dispatch commit when the input is omitted. Verify the receipt and archive in the same signed bundle, then compare the source, platform and archive bytes to the receipt.

```bash
set -euo pipefail
for subject in "$EVIDENCE/receipt.json" "$ARCHIVE"; do
  gh attestation verify "$subject" --repo cofy-x/axern \
    --signer-workflow cofy-x/axern/.github/workflows/native-provenance.yml \
    --source-ref refs/heads/main --source-digest "$WORKFLOW_COMMIT" \
    --deny-self-hosted-runners --bundle "$EVIDENCE/provenance.jsonl"
done
digest=$(sha256sum "$ARCHIVE" | cut -d' ' -f1)
jq -e --arg commit "$CANDIDATE_COMMIT" --arg arch "$ARCH" --arg digest "$digest" '
  .schema == "axern.native-delivery-inventory.v1" and
  .source_revision == $commit and .architecture == $arch and .archive_sha256 == $digest
' "$EVIDENCE/receipt.json"
```

For SBOMs, also match their bytes to the inventory digests in the verified receipt. For the checksums file, compare its entry for the published archive name to the verified archive digest.

```bash
(cd "$EVIDENCE" && jq -er '.inventory_sha256 | to_entries[] | "\(.value)  \(.key)"' receipt.json | sha256sum --check --strict)
```

Native installation is experimental. The installation guide lists the [exact source builds accepted for a cold upgrade](INSTALL.md#qualify-an-experimental-archive-transition) and explains how to [restore a drained, stopped installation on a replacement host](INSTALL.md#qualify-a-replacement-node) after fencing the original. Published-version upgrades, Compose migration and macOS VM provisioning remain outside this qualification. Record the host, archive identities, commands and results of the relevant runtime or recovery checks.
