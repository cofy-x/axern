---
title: Local Axern
description: Run and manage a source-free, machine-level Axern instance.
---

`axern local up` is the supported local installation. The CLI contains the
version-matched deployment bundle, generates local identity material, invokes
Docker Compose v2, and configures the public gateway context.

It is a machine-level instance: you can run the command from any directory and
do not need an Axern project file.

## Start and inspect

```bash
axern local up
axern local image load python:3.12-slim --pull
axern local status
```

Starting again is idempotent. Existing PostgreSQL, object, runtime, and
identity data is preserved. If another context is already selected, `local up`
does not replace it; use `axern local up --use` when you want to switch.

Optional local telemetry is deliberately excluded from the cold-start path:

```bash
axern local up --profile observability
```

## Diagnose and read logs

```bash
axern local doctor
axern local logs
axern local logs gatewayd --follow --tail 100
```

`doctor` is read-only and reports an executable recommendation for each failed
check. Use `--output json` with `status` or `doctor` in automation.

## Stop, upgrade, or delete

```bash
axern local down
axern local upgrade
axern local reset
```

`down` removes containers and the network but keeps data. Upgrades are always
explicit and create a local backup before migration. `reset` permanently
deletes the instance and requires interactive confirmation, or `--force` in
CI.

To locate the data without relying on platform-specific paths:

```bash
axern local path
```

See the [`axern local` reference](/guides/local/) for ports, paths, proxy
handling, failure recovery, and complete command behavior.

## Developing Axern itself

Repository Compose scripts and source-built `:dev` images are contributor
tools, not an installation path. If you are changing Axern, follow the
[contributor guide](https://github.com/cofy-x/axern/blob/main/CONTRIBUTING.md)
and the local deployment README in the repository.
