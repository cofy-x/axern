# PostgreSQL Image

This image derives from the pinned official PostgreSQL 16 Alpine image. It refreshes signed packages from the base image's stable Alpine repositories and replaces its gosu privilege-switching helper with pinned upstream source built using the repository's fixed Go toolchain and dependency checksums. The final image contains no Go SDK or build tools.

The official `docker-entrypoint.sh`, `postgres` command, PostgreSQL user and group (UID/GID 70), `PGDATA=/var/lib/postgresql/data`, volume declaration, initialization environment variables and initialization script directory remain unchanged. Existing PostgreSQL 16 data uses the same directory and ordinary entrypoint checks. This is not a major-version data migration or a qualified backup/restore procedure. Native Linux installation uses operator-managed host PostgreSQL instead of this image.

Prepare each architecture from the repository root, then build from its isolated generated context:

```bash
python3 deploy/images/postgres/build.py --check
python3 -m unittest discover -s deploy/images/postgres -p 'test_*.py'
python3 deploy/images/postgres/build.py --arch amd64 --go /path/to/go1.26.10/bin/go --output deploy/images/postgres/.build/amd64-qualification-1
docker build --platform linux/amd64 -f deploy/images/postgres/Dockerfile --build-arg APK_REFRESH_EPOCH=qualification-1 -t axern/local-postgres:dev deploy/images/postgres/.build/amd64-qualification-1/context
```

Use `--arch arm64` for the other supported architecture. Each output directory must be new and contains a `context/` with only gosu, its license, source pins, module locks and build receipt. An existing source archive can be supplied with `--source-archive`; its size and SHA-256 are still verified. The helper build uses `CGO_ENABLED=0`, `GOWORK=off`, `GOTOOLCHAIN=local` and `-mod=readonly`; it never updates dependency versions. `--prepare-only --output /new/audit-directory` materializes the exact verified source and committed module locks under `source/` for independent vulnerability analysis without compiling.

`APK_REFRESH_EPOCH` invalidates the package-update build layer for a new qualification or release. Stable package versions can change without a source commit; qualify the actual resulting image digest. The image records installed APK versions, source pins, helper module locks and helper build metadata under `/usr/local/share/axern/postgres/`. A scanner result is specific to that digest and database snapshot; package-level matches alone do not prove a reachable PostgreSQL server vulnerability.

Deployment health checks should continue using the official `pg_isready` executable with the deployment's database user and database name. Qualification should initialize a task-owned volume using the official base image, write and checkpoint a SQL marker, stop it cleanly, then start and restart the derived image on that same volume and verify both `pg_isready` and the unchanged marker. Test with private task-owned credentials and avoid exposing a host database port.
