# Native Linux Runtime Payload

This directory documents the packaging foundation for [native Linux and Lima deployment](https://github.com/cofy-x/axern/issues/203), under the [standalone serving workstream](https://github.com/cofy-x/axern/issues/197). The artifact is an opt-in build candidate. It does not install services, change `axern local up`, implement persistent Workspaces, or qualify Linux/macOS serving. Existing Compose and Helm deployment paths remain unchanged.

The intended next consumers are a Linux standalone installer and the same installer inside one Axern-managed Lima VM on macOS. They must use the existing `Environment -> Run -> Allocation` lifecycle, PostgreSQL authority and gVisor/runsc execution boundary. Lima supplies a Linux host; it is not another sandbox backend.

## Build

Build natively on Ubuntu 24.04, on amd64 or arm64, from a clean committed checkout. Cross-compilation and other build distributions are rejected by the initial profile. Use the repository's Go version and Rust toolchain with the locked dependencies; Rust's native dependencies require a C/C++ compiler, CMake, pkg-config, OpenSSL development headers and protoc. Dependencies and generated protobuf prerequisites follow the existing repository bootstrap and module build instructions.

Obtain the full `gvisor.tar.bz2` archive selected by [gvisor.lock](../../runtime/axnoded/gvisor.lock). The upstream URL is `https://storage.googleapis.com/gvisor/releases/release/<AXERN_GVISOR_RELEASE>/<x86_64-or-aarch64>/gvisor.tar.bz2`. The builder checks the architecture-specific SHA-512 before compiling and retains the complete archive without unpacking or executing it.

```bash
make native-runtime-check
make native-runtime-build GVISOR_ARCHIVE=/absolute/path/to/gvisor.tar.bz2
```

The builder compiles the Go service entrypoints and the locked Rust `imagefsd` package. It uses temporary staging, keeps the Rust build cache under `target/native-runtime`, and publishes to `dist/native-runtime/axern-runtime_<version>_linux_<arch>.tar.gz` only after verification. It refuses to replace an existing output and does not remove another build's artifacts. To compare multiple builds, use a new output directory:

```bash
python3 scripts/release/native_runtime.py build \
  --gvisor-archive /absolute/path/to/gvisor.tar.bz2 \
  --output-dir /absolute/path/to/new-candidate-directory
```

The final JSON receipt reports the archive path and SHA-256. Build output goes to stderr except for compiler/tool output. This receipt records the candidate; it is not a signed provenance attestation. A clean source revision is checked before and after building, and executable headers are checked for the declared architecture. Packaging cannot establish runtime correctness or reproduce the identity of dependencies outside the locked source and supplied runtime archive.

## Payload Contract

`manifest.json` is the first archive member. Its `axern.native-runtime.v1` schema records the version, source commit, Linux architecture, candidate build profile/libc, gVisor source/release/archive digest and the exact file inventory with sizes, SHA-256 hashes and modes. Entries use deterministic order, normalized ownership/modes and source-commit timestamps; gzip omits the output filename and wall-clock timestamp. This makes packaging the same staged bytes reproducible, without claiming every compiler produces reproducible binaries.

| Payload | Purpose |
| --- | --- |
| `bin/` | CLI, control/migration/access-bootstrap/retention, gateway, node, image manager/filesystem, egress, tunnel and diagnostic binaries |
| `libexec/axnoded/` | Sandbox supervisor, egress/DNS probes and sandbox tunnel agent |
| `runtime/gvisor.tar.bz2` and `share/axern/gvisor.lock` | Full pinned upstream runtime, including its helper directory; the installer must preserve its layout |
| `share/axern/runtime-selftest/memory-hog` and `share/axern/erofs-root/` | Axern-owned capability fixture inputs for installer assembly |
| `share/axern/nydus_registry.json.example` and `share/axern/registry_auths.json` | Existing image-backend template and an empty credential map |
| `requirements.json`, `LICENSE`, `NOTICE` | Candidate host requirements and Axern license notices |

PostgreSQL, system libraries, Linux utilities and BusyBox are host prerequisites, not bundled executables. The installer must assemble the capability rootfs using host `busybox-static` and the bundled memory-hog, and generate the EROFS fixture from its source directory. This avoids copying arbitrary host binaries into the release payload. Imagefsd remains included because imagemgr currently requires it even on ordinary OCI paths; making it optional is separate work.

The payload contains no node identity, TLS/private keys, database, registry credentials, mutable workspace, service unit or installation configuration. The fixed inventory rejects additional files rather than recursively archiving a developer's directory. Existing module flags/default paths still require installer-owned wiring; unpacking these files does not establish a working standalone stack.

## Verify Without Installation

Use an expected SHA-256 from a trusted build receipt or future authenticated release metadata. Computing a checksum from an unknown download and passing that same checksum proves no origin.

```bash
make native-runtime-verify \
  RUNTIME_ARCHIVE=/absolute/path/to/axern-runtime_VERSION_linux_amd64.tar.gz \
  RUNTIME_SHA256=EXPECTED_SHA256_FROM_TRUSTED_RECEIPT
```

The verifier reads the archive without extraction or execution. It checks the outer digest, bounded manifest, closed inventory, per-file size/mode/hash, ELF architecture and the nested gVisor archive against the bundled lock. Duplicate names, unexpected files, links, special files, credential-bearing registry templates and missing members fail. Passing verifies payload structure and integrity; it does not authenticate the publisher, inspect dynamic library compatibility, or prove sandbox isolation.

## Validation And Remaining Gates

`make native-runtime-check` uses small synthetic ELF headers and fixture data to exercise the archive boundary. It does not download gVisor, compile services, or run a sandbox. The check is part of `make release-check`, so the existing release-contract CI job runs it. A real native build and archive verification provide separate packaging evidence.

This candidate is deliberately outside `make release-build` and automated publication. Extending the release channel requires a payload-aware dependency/license inventory, required third-party notices, provenance/checksum publication, native architecture builds and qualified installation tests. The source-tree SBOM alone is not a claim that the complete binary/runtime archive has passed the [dependency license policy](../../docs/legal/dependency-licenses.md).

The next implementation must provide systemd supervision, privilege separation, owned paths, secret/identity bootstrap, migration ordering, the complete runtime installation, capability fixture generation and real resource/network enforcement. It must also define stop/start preservation and the supported upgrade/export boundary. Lima provisioning, CLI lifecycle integration and Compose migration follow that common Linux foundation. Real macOS tests are required before claiming Lima support or changing the default local deployment; Linux mocks and package checks cannot establish that support. Track implementation PRs and actual platform evidence in [#203](https://github.com/cofy-x/axern/issues/203).
