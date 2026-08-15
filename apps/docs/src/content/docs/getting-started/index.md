---
title: Quick Start
description: Install Axern and run an isolated command locally without cloning source code.
---

Run a complete Axern stack on your machine without cloning the repository or
installing Go, Node.js, Helm, `kubectl`, Make, or OpenSSL.

## Prerequisites

- macOS or Linux on amd64 or arm64
- Docker Desktop, or Docker Engine with Compose v2
- At least 4 CPU cores, 8 GiB of memory, and 20 GiB of free disk space

## 1. Install the CLI

```bash
brew install cofy-x/tap/axern
```

If you do not use Homebrew:

```bash
curl -fsSL https://raw.githubusercontent.com/cofy-x/axern/main/install.sh | sh
```

The installer downloads a GitHub Release archive, verifies it against
`checksums.txt`, and installs into a user-writable directory. Set
`AXERN_VERSION` or `AXERN_INSTALL_DIR` to override its defaults.

## 2. Start Local Axern

```bash
axern local up
```

The command checks Docker and host resources, starts only the core services,
waits until the gateway and runtime are healthy, and creates the `local`
context. Host Docker images are not implicitly shared with the node; load the
image used by the first workload:

```bash
axern local image load python:3.12-slim --pull
```

## 3. Run a command

```bash
axern run python:3.12-slim -- python -c 'print("hello from axern")'
```

Axern streams the command's stdout and stderr to your terminal. The CLI exits
with the command's real exit code. Every execution also creates a durable Run
record that you can inspect later:

```bash
axern run list
axern run logs <run-id>
```

Run status is durable. Output streaming is currently backed by node-local
files and is available only while that allocation output is retained; durable
seven-day output retention is a separate storage capability.

## Next steps

- Learn how to stop, diagnose, reset, and upgrade the stack in [Local Axern](/getting-started/compose/).
- Define reusable, reviewable executions in [Runs](/guides/run/).
- See the complete lifecycle reference in [`axern local`](/guides/local/).
- Use [Kubernetes installation](/getting-started/kubernetes/) for shared or production deployments.

:::caution[Local development boundary]
The local stack binds public ports to `127.0.0.1` and generates a development
identity for one machine. Do not expose it as a shared or production service.
:::
