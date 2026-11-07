# Native Linux Runtime Payload

This directory owns the native Linux installation assets and operating instructions. The runtime archive contains Axern services, helpers, a pinned gVisor runtime and an experimental systemd installer. See [Native Installation](INSTALL.md) for prerequisites, configuration, startup, shutdown and identity renewal.

The installation uses the existing `Environment -> Run -> Allocation` lifecycle, PostgreSQL authority and gVisor/runsc execution boundary. It requires a Linux host. It does not provision Lima, manage `axern local` contexts or implement persistent Workspace APIs.

Use the opt-in [startup and idle measurement](MEASUREMENT.md) harness to record the installed candidate's startup phases, first workload output and cgroup/process resource footprint on an exclusive disposable host.

## Build

Build natively on Ubuntu 24.04, on amd64 or arm64, from a clean committed checkout. Cross-compilation and other build distributions are rejected by the initial profile. Use the repository's Go version with locked dependencies. Generated protobuf prerequisites follow the existing repository bootstrap and module build instructions. The native payload uses OCI-only imagemgr and does not build or package Rust/imagefsd.

Obtain the full `gvisor.tar.bz2` archive selected by [gvisor.lock](../../runtime/axnoded/gvisor.lock). The upstream URL is `https://storage.googleapis.com/gvisor/releases/release/<AXERN_GVISOR_RELEASE>/<x86_64-or-aarch64>/gvisor.tar.bz2`. The builder checks the architecture-specific SHA-512 before compiling and retains the complete archive without unpacking or executing it.

```bash
make native-runtime-check
make native-runtime-build GVISOR_ARCHIVE=/absolute/path/to/gvisor.tar.bz2
```

The builder compiles the Go service entrypoints, uses temporary staging, and publishes to `dist/native-runtime/axern-runtime_<version>_linux_<arch>.tar.gz` only after verification. It refuses to replace an existing output and does not remove another build's artifacts. To compare multiple builds, use a new output directory:

```bash
python3 scripts/release/native_runtime.py build \
  --gvisor-archive /absolute/path/to/gvisor.tar.bz2 \
  --output-dir /absolute/path/to/new-candidate-directory
```

The final JSON receipt reports the archive path and SHA-256. Build output goes to stderr except for compiler/tool output. This receipt records the candidate; it is not a signed provenance attestation. A clean source revision is checked before and after building, and executable headers are checked for the declared architecture. Packaging cannot establish runtime correctness or reproduce the identity of dependencies outside the locked source and supplied runtime archive.

## Payload Contract

`manifest.json` is the first archive member. Its `axern.native-runtime.v2` schema records the version, source commit, Linux architecture, candidate build profile/libc, gVisor source/release/archive digest and the exact file inventory with sizes, SHA-256 hashes and modes. Entries use deterministic order, normalized ownership/modes and source-commit timestamps; gzip omits the output filename and wall-clock timestamp. This makes packaging the same staged bytes reproducible, without claiming every compiler produces reproducible binaries.

| Payload | Purpose |
| --- | --- |
| `bin/` | CLI, control/migration/access-bootstrap/retention, gateway, node, OCI image manager, egress, tunnel and diagnostic binaries |
| `libexec/axnoded/` | Sandbox supervisor, egress/DNS probes and sandbox tunnel agent |
| `libexec/axern/` | Native installer, payload verification/staging tool and opt-in qualification runners |
| `runtime/gvisor.tar.bz2` and `share/axern/gvisor.lock` | Full pinned upstream runtime, including its helper directory; the installer must preserve its layout |
| `share/axern/runtime-selftest/memory-hog` and `share/axern/erofs-root/` | Axern-owned capability fixture inputs for installer assembly |
| `share/axern/registry_auths.json` | Empty OCI registry credential map |
| `requirements.json`, `LICENSE`, `NOTICE` | Candidate host requirements and Axern license notices |

PostgreSQL, Python, system libraries, Linux utilities and BusyBox are host prerequisites, not bundled executables. The installer assembles the capability rootfs using host `busybox-static` and the bundled memory-hog, and generates the EROFS fixture from its source directory. This avoids copying arbitrary host binaries into the release payload. Imagemgr starts with `-enable_nydus=false`; Nydus images are rejected and no imagefsd service, template or binary is required.

The payload contains no node identity, TLS/private keys, database, registry credentials or mutable workspace. The fixed inventory rejects additional files rather than recursively archiving a developer's directory. The installer generates host-specific configuration and systemd units; staging files alone does not start services.

## Verify Without Installation

Use an expected SHA-256 from a trusted build receipt or future authenticated release metadata. Computing a checksum from an unknown download and passing that same checksum proves no origin.

```bash
make native-runtime-verify \
  RUNTIME_ARCHIVE=/absolute/path/to/axern-runtime_VERSION_linux_amd64.tar.gz \
  RUNTIME_SHA256=EXPECTED_SHA256_FROM_TRUSTED_RECEIPT
```

The verifier reads the archive without extraction or execution. It checks the outer digest, bounded manifest, closed inventory, per-file size/mode/hash, ELF architecture and the nested gVisor archive against the bundled lock. Duplicate names, unexpected files, links, special files, credential-bearing registry templates and missing members fail. Passing verifies payload structure and integrity; it does not authenticate the publisher, inspect dynamic library compatibility, or prove sandbox isolation.

## Verification And Support Boundary

`make native-runtime-check` exercises corruption, architecture checks, concurrent staging, configuration validation, generated systemd units and data-identity protection. Its archive tests use synthetic ELF headers; it does not download gVisor, compile services or run a sandbox. The check is part of `make release-check`. Real native builds, service operation and sandbox enforcement require the separate [fresh-archive qualification](QUALIFICATION.md).

This candidate is deliberately outside `make release-build` and automated publication. Extending the release channel requires a payload-aware dependency/license inventory, required third-party notices, provenance/checksum publication, native architecture builds and qualified installation tests. The source-tree SBOM alone is not a claim that the complete binary/runtime archive has passed the [dependency license policy](../../docs/legal/dependency-licenses.md).

Native installation is experimental. Supported upgrades, coordinated backup/restore, Compose migration and macOS VM provisioning are unavailable. Record acceptance commands, platform details and source revisions in the implementation PR or operational test record. Packaging checks do not establish sandbox enforcement or durable serving qualification.
