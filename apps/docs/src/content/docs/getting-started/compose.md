---
title: Compose Development
description: Run and manage a source-free Docker Compose development instance.
---

`axern dev up` manages the Docker Compose development profile. The CLI contains the version-matched deployment bundle, generates local identity material, invokes Docker Compose v2, and configures the public gateway context.

It is a machine-level instance: you can run the command from any directory and do not need an Axern project file.

## Start and inspect

```bash
axern dev up
axern dev image load python:3.12-slim --pull
axern dev status
```

Starting again is idempotent. Existing PostgreSQL, runtime, and identity data is preserved. If another context is already selected, `dev up` does not replace it; use `axern dev up --use` when you want to switch.

Telemetry export is disabled by default. Configure an operator-owned OTLP receiver reachable from Docker containers when needed; see the [Compose development reference](/guides/local/).

## Diagnose and read logs

```bash
axern dev doctor
axern dev logs
axern dev logs gatewayd --follow --tail 100
```

`doctor` is read-only and reports an executable recommendation for each failed check. Use `--output json` with `status` or `doctor` in automation.

## Stop or rebuild

```bash
axern dev down
axern dev reset --force
axern dev up
```

`down` removes containers and the network but keeps data. Local state is intentionally rebuild-only across versions: export required outputs, then use `reset --force` and `up`. `reset` permanently deletes the instance and requires interactive confirmation without `--force`.

To locate the data without relying on platform-specific paths:

```bash
axern dev path
```

See the [`axern dev` reference](/guides/local/) for ports, paths, proxy handling, failure recovery, and complete command behavior.

## Developing Axern itself

Repository Compose scripts and source-built `:dev` images are contributor tools, not an installation path. If you are changing Axern, follow the [contributor guide](https://github.com/cofy-x/axern/blob/main/CONTRIBUTING.md) and the local deployment README in the repository.
