---
title: Quick Start
description: Start a coherent Axern source build and choose native serving or Compose development.
---

The current source introduces a coordinated CLI, server and SDK protocol change. A source version does not establish published package availability. Use one matching build instead of mixing a previous release client with current services.

## Linux single-host serving

Build and install a complete archive using the repository's [native installation guide](https://github.com/cofy-x/axern/blob/main/deploy/standalone/INSTALL.md). The target needs no source checkout, compiler, Docker or Kubernetes. Persistent Workspaces require a dedicated XFS data mount, project quotas and an explicit storage budget.

After installation, operate this host through the archive's CLI:

```bash
sudo /opt/axern/releases/<archive-sha256>/bin/axern server start
sudo /opt/axern/releases/<archive-sha256>/bin/axern server status
sudo /opt/axern/releases/<archive-sha256>/bin/axern server logs axnoded
```

API contexts select the endpoint for execution requests. They do not turn `server` commands into remote host administration.

## Compose source development

Use Go 1.26.10, Make and Docker Compose v2 on macOS or Linux. At least 4 CPU cores, 8 GiB memory and 20 GiB free disk are recommended. From the source repository root:

```bash
make quickstart-source
make axern-cli-build
./bin/axern context list
```

This builds current source images and runs the Compose smoke. The [local deployment entrypoint](https://github.com/cofy-x/axern/blob/main/deploy/local/README.md) defines connection and context setup. The development stack and native serving installation have independent data.

A published CLI's embedded development bundle uses `axern dev up|status|logs|down` and needs matching published service images. It does not consume the source checkout's builds. See [Compose development](/getting-started/compose/) for that lifecycle.

## Execute and retain work

- [Runs](/guides/run/) create execution, expose state and provide bounded logs.
- [Workspaces](/guides/workspaces/) retain files for a fresh Run after the old writer is confirmed cleaned up.
- [SDKs](/sdk/) expose Go, Python sync/async and TypeScript APIs.

Without a Workspace, writable files belong to the current Allocation. Sealed logs and declared outputs expire 15 minutes after cleanup starts. Workspace capacity and deletion are independent; supported backup, upgrades and replacement-host restore remain unqualified.
