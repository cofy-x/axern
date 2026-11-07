# Agent Workload Images

Axern maintains development and Agent workload images in this directory. A single Dockerfile builds a public Python base, a shared Go/Node/Python development toolchain, and independent official Claude Code and Codex layers. The images contain programs and tools; callers provide tasks, model providers, runtime credentials, session policy and working-directory inputs. No model configuration, account state, API keys or experiment launcher is embedded.

## Layers and versions

| Target | Contents | Default local image tag |
| --- | --- | --- |
| `base` | Digest-pinned Python on Debian bookworm, Bash, Git, ripgrep, CA certificates and UID/GID 1000 | `axern-agent-base:local` |
| `toolchain` | Base plus fixed Go and Node, Python, C/C++ build tools, Make and pkg-config | `axern-agent-toolchain:local` |
| `claude-code` | The complete shared toolchain plus official Claude Code wrapper and native package | `axern-agent-claude:<locked-version>-local` |
| `codex` | The complete shared toolchain plus the official Codex platform package and complete vendor resources | `axern-agent-codex:<locked-version>-local` |

[versions.lock.json](versions.lock.json) is the single version source for the base image, Python, Go, Node and Agent distributions, their public URLs, exact sizes and checksums. Go follows the repository's `go.work` version. The initial lock targets Linux/amd64; this does not qualify Linux/arm64 or macOS execution. macOS clients can use the images through a Linux Axern host.

Both Agent targets inherit the same completed `toolchain` stage. Updating one Agent does not reinstall its own copy of the development tools. Additional Agent families should add a separate stage above that shared toolchain, their official distribution entries in the lock, installation and smoke coverage. Keep model integrations in the calling application.

## Build

Use the repository's stable commands from its root:

```bash
make agent-images-check
make agent-images
make agent-images-smoke
```

`agent-images-check` needs Python 3.12 or newer and runs host-safe lock, checksum, archive, cache and context checks without Docker or model access. Builds require Docker with BuildKit and internet access to the public locked sources and Debian repositories. The build wrapper supplies the base reference from the lock; building the Dockerfile directly requires the same `BASE_IMAGE` argument.

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

Debian packages use signed repositories and their actual versions are recorded inside each image under `/usr/local/share/axern/agent-images/`. Those repositories are not snapshot-pinned, so rebuilding is not claimed to reproduce identical image bytes. BuildKit caches common layers; build receipts record the exact source hashes. Publishing these local tags to a registry is a separate operator action. Resolve and retain the resulting manifest digest before selecting an image for an Axern Environment.

## Runtime contract

All targets default to `USER 1000:1000`, `HOME=/home/agent`, `WORKDIR /workspace` and a Bash command. The caller must select and verify Axern's actual execution user and provide a writable working directory and HOME. Image defaults do not create a persistent Workspace, authorize a host path or grant shared writes.

The toolchain exposes `python3`, `node`, `npm`, `npx`, `go` and `gofmt` on the ordinary PATH. Agent images additionally expose `/usr/local/bin/claude` or `/usr/local/bin/codex`. Claude retains both official packages and their license files under `/opt/claude-code`. Codex retains its package metadata, complete vendor directory, upstream license and notice under `/opt/codex`; sibling resources and third-party notices remain available to the native program. Go and Node retain their upstream distributions and licenses.

The images install the official programs without model or permission presets. A calling application selects the model endpoint, supplies credentials through runtime Secret projection, chooses Agent flags and tool policy, owns bounded execution and interprets the terminal result. Axern's gVisor, mounts, network policy and resource controls define the execution boundary. Agent-private conversation state and retained project files remain separate concepts.

## Verification and source references

The Docker smoke uses networking disabled, dropped capabilities and UID 1000. It checks each selected image's real version/help, Git/ripgrep, Go compilation, Node execution, compiler availability and expected runtime assets. When the toolchain and Agent targets are selected together, it also compares their actual filesystem layer identities to verify toolchain sharing. These checks establish image contents and ordinary container startup; actual Axern execution, model compatibility, task completion and persistent Workspace behavior require separate integration acceptance.

Distribution and installation behavior follow the official [Python image](https://hub.docker.com/_/python), [Go releases](https://go.dev/dl/), [Node releases](https://nodejs.org/en/download), [Claude Code installation](https://code.claude.com/docs/en/setup) and [Codex CLI](https://developers.openai.com/codex/cli/) documentation. The lock records the exact selected artifacts rather than installing a floating latest version.
