# Agent Workload Images

Axern maintains development and Agent workload images in this directory. A single Dockerfile builds a public Python base, a shared Go/Node/Python development toolchain, and independent official Claude Code and Codex layers. The images contain programs and tools; callers provide tasks, model providers, runtime credentials, session policy and working-directory inputs. No model configuration, account state, API keys or experiment launcher is embedded.

## Layers and versions

| Target | Contents | Default local image tag |
| --- | --- | --- |
| `base` | Digest-pinned Python on Debian 13 trixie, checksum-pinned pip, Bash, Git, ripgrep, CA certificates and UID/GID 1000 | `axern-agent-base:local` |
| `toolchain` | Base plus fixed Go and the Node runtime, Python, C/C++ build tools, Make and pkg-config | `axern-agent-toolchain:local` |
| `claude-code` | The complete shared toolchain plus official Claude Code wrapper and native package | `axern-agent-claude:<locked-version>-local` |
| `codex` | The complete shared toolchain plus the official Codex platform package and complete vendor resources | `axern-agent-codex:<locked-version>-local` |

[versions.lock.json](versions.lock.json) is the single version source for the base image, Debian, Python, pip, Go, Node and Agent distributions, their public URLs, exact sizes and checksums. The base upgrades pip from a verified wheel without index access or dependency resolution; the Python image's bundled installer is not the runtime version contract. Go follows the repository's `go.work` version. The lock targets Linux/amd64; this does not qualify Linux/arm64 or macOS execution. macOS clients can use the images through a Linux Axern host.

Both Agent targets inherit the same completed `toolchain` stage. Updating one Agent does not reinstall its own copy of the development tools. Additional Agent families should add a separate stage above that shared toolchain, their official distribution entries in the lock, installation and smoke coverage. Keep model integrations in the calling application.

## Build

Use the repository's stable commands from its root:

```bash
make agent-images-check
make agent-images
make agent-images-smoke
```

`agent-images-check` needs Python 3.12 or newer and runs host-safe lock, checksum, archive, cache and context checks without Docker or model access. Builds require Docker with BuildKit and internet access to the public locked sources and Debian repositories. Direct Dockerfile builds require the locked `BASE_IMAGE` and a fresh `APT_REFRESH_EPOCH` build argument.

The default targets are `base,toolchain,claude-code,codex`. Choose targets, a local tag namespace and fresh receipt directories explicitly when needed:

```bash
make agent-images \
  AGENT_IMAGE_TARGETS=toolchain,claude-code,codex \
  AGENT_IMAGE_TAG_PREFIX=axern-agent \
  AGENT_IMAGE_TAG_SUFFIX=local \
  AGENT_IMAGE_OUTPUT_DIR=/absolute/path/to/new-build-receipts

make agent-images-smoke \
  AGENT_IMAGE_TARGETS=toolchain,claude-code,codex \
  AGENT_IMAGE_TAG_PREFIX=axern-agent \
  AGENT_IMAGE_TAG_SUFFIX=local \
  AGENT_IMAGE_OUTPUT_DIR=/absolute/path/to/new-smoke-receipts
```

`AGENT_IMAGE_CACHE_DIR` optionally supplies previously downloaded archives under their original URL basenames. The wrapper copies only listed files into an isolated read-only BuildKit context and validates their exact size and hashes. A corrupt or symlinked cached input fails; an absent input is downloaded from its locked HTTPS source. `AGENT_IMAGE_DOCKER_CONFIG` optionally selects the caller's Docker configuration directory; that directory is never a build input. The main context contains only the Dockerfile, lock and installation helper, with an explicit allowlist.

The base applies available Debian package updates from signed repositories before installing common tools. Each build invocation refreshes Apt once per common stage and shares that refresh across all selected targets. The base and toolchain remove setuid/setgid file bits from installed `/usr` tools. Actual package versions are recorded inside each image under `/usr/local/share/axern/agent-images/`. The signed repositories update independently of the source commit. Build receipts retain the exact source hashes, Apt refresh identity, platform, and immutable Docker image ID. Publishing local images to a registry is a separate operator action. Resolve and retain the resulting registry manifest digest before selecting an image for an Axern Environment.

## Audit the built images

Use the same target selection as the build. Initial inventory requires a Docker image store that can export the OCI platform selected by `docker image inspect --platform` and `docker image save --platform`. Prepare a fresh database and save results in a new directory.

```bash
bash scripts/release/install-image-auditors.sh /absolute/path/to/auditors
GRYPE_DB_CACHE_DIR=/absolute/path/to/grype-db /absolute/path/to/auditors/grype db update
make agent-images-audit \
  AGENT_IMAGE_BUILD_RECEIPTS=/absolute/path/to/new-build-receipts/build-receipts.json \
  AGENT_IMAGE_AUDIT_TOOLS=/absolute/path/to/auditors \
  AGENT_IMAGE_AUDIT_DB=/absolute/path/to/grype-db \
  AGENT_IMAGE_OUTPUT_DIR=/absolute/path/to/new-audit-receipts
```

The audit verifies each image selected by its build receipt, including the platform, manifest, config, and layers. It preserves the raw SBOM and all vulnerability matches, including the scanner's default ignored findings. Each image records the SHA-256 of its `syft.json` as `sbom_digest`. Save the `summary_digest` printed on stdout separately from the audit directory. It identifies the finished `summary.json`. An unsuccessful or mismatched scan leaves an incomplete result and exits nonzero. A complete result means the scan succeeded. Review fixes and package or feature applicability against the maintained upstream before treating a workload as qualified. A generic SBOM cannot establish all dependencies embedded in the native Agent binaries.

To rescan the same verified inventory with a refreshed Grype database, use the original build receipts and target selection. Pass the prior audit directory and its independently saved `summary_digest`.

```bash
make agent-images-audit \
  AGENT_IMAGE_BUILD_RECEIPTS=/absolute/path/to/build-receipts.json \
  AGENT_IMAGE_AUDIT_TOOLS=/absolute/path/to/auditors \
  AGENT_IMAGE_AUDIT_DB=/absolute/path/to/refreshed-grype-db \
  AGENT_IMAGE_OUTPUT_DIR=/absolute/path/to/new-audit-receipts \
  AGENT_IMAGE_REUSE_INVENTORY=/absolute/path/to/prior-audit-receipts \
  AGENT_IMAGE_INVENTORY_DIGEST="$saved_summary_digest"
```

The rescan verifies the prior summary, its build receipts and each SBOM's digest and image identity. It copies the verified inventories into the new directory and scans them with the selected database. This path needs the pinned audit tools and no Docker image store. The new summary records the prior summary's digest and can serve as the basis for another rescan. For an older audit without per-image SBOM digests, first run a full audit.

## Runtime contract

All targets default to `USER 1000:1000`, `HOME=/home/agent`, `WORKDIR /workspace` and a Bash command. The caller must select and verify Axern's actual execution user and provide a writable working directory and HOME. Image defaults do not create a persistent Workspace, authorize a host path or grant shared writes.

The toolchain exposes `python3`, `pip`, `node`, `go` and `gofmt` on the ordinary PATH. Node retains its runtime, headers, documentation and license; its bundled npm/Corepack package trees and package-manager launchers are removed before the runtime enters an image layer. No npm, npx, Corepack, pnpm or Yarn is provided. Project package managers and their dependency graphs belong in explicitly versioned caller-owned derived images. Agent images additionally expose `/usr/local/bin/claude` or `/usr/local/bin/codex`. Claude retains both official packages and their license files under `/opt/claude-code`. Codex retains its package metadata, complete vendor directory, upstream license and notice under `/opt/codex`; sibling resources and third-party notices remain available to the native program. Go retains its upstream distribution and licenses.

The images install the official programs without model or permission presets. A calling application selects the model endpoint, supplies credentials through runtime Secret projection, chooses Agent flags and tool policy, owns bounded execution and interprets the terminal result. Clearing image privilege bits is separate from Axern's enforced process privilege policy; explicit root caller setup remains possible. Axern's gVisor, mounts, network policy and resource controls define the execution boundary. Agent-private conversation state and retained project files remain separate concepts.

## Verification and source references

The Docker smoke uses networking disabled, dropped capabilities and UID 1000. It checks each selected image's real Python and pip versions, Agent version/help, Git/ripgrep, Go compilation, Node execution, absent package-manager binaries and vendor trees, compiler availability and expected runtime assets. A separate root metadata scan, with only DAC override capability, checks all regular files outside the virtual `/proc`, `/sys` and `/dev` trees for remaining setuid/setgid bits and fails on unreadable paths. When the toolchain and Agent targets are selected together, it also compares their actual filesystem layer identities to verify toolchain sharing. These checks establish image contents and ordinary container startup; native privilege enforcement, Axern execution, model compatibility, task completion and persistent Workspace behavior require separate integration acceptance. Source lockfile auditing does not audit Debian packages or bundled dependencies in these images; retain an image SBOM and inspect vulnerability findings separately.

Distribution and installation behavior follow the official [Python image](https://hub.docker.com/_/python), [Go releases](https://go.dev/dl/), [Node releases](https://nodejs.org/en/download), [Claude Code installation](https://code.claude.com/docs/en/setup) and [Codex CLI](https://developers.openai.com/codex/cli/) documentation. The lock records the exact selected artifacts rather than installing a floating latest version.
