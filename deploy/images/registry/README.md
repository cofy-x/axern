# Axern Registry image

This image serves the OCI Distribution API for Axern development and artifact publication. It exposes the filesystem storage driver and builds pinned Distribution source with a committed dependency overlay and Go toolchain, then ships only static Registry and health-check executables, configuration, a dated Mozilla CA bundle, provenance and license notices. It has no shell, package manager or development tools.

`artifacts.lock.json` binds the upstream module, release, commit, source archive, CA bundle, license and overlay checksums. `entrypoint.patch` removes cloud-provider and volatile in-memory storage registration. Source preparation excludes the corresponding Azure, GCS, S3 and CloudFront source directories. `go.mod` and `go.sum` retain the complete dependency graph of the supported source profile, including its tests and foreign-platform sources. Source preparation excludes the upstream `vendor` copy so it cannot override the dependency overlay; builds use read-only module mode. Dependency updates are made to these tracked files before building, never resolved with `go get` during a build.

## Build

Use Python 3.11 or newer, the locked Go version and Docker for image assembly. All outputs must use a new directory; existing build and failure evidence is preserved.

```bash
python3 deploy/images/registry/build.py --check
python3 deploy/images/registry/build.py --prepare-only --go /path/to/go --output /private/registry-source
python3 deploy/images/registry/build.py --arch amd64 --go /path/to/go --output /private/registry-amd64
```

The preparation command produces `DIR/source` for independent source and full-module security checks. Normal mode also produces `DIR/context`. The Dockerfile consumes that generated context:

```bash
docker build --platform linux/amd64 -f deploy/images/registry/Dockerfile -t axern/local-registry:dev /private/registry-amd64/context
```

Use `--arch arm64` and the matching Docker platform for an arm64 image. The build verifies ELF architecture and absence of a dynamic interpreter. The generated provenance records the input identities and executable checksums; final-image SBOM and vulnerability scans remain required.

## Runtime and retained data

Cloud storage and in-memory storage configurations fail as unknown drivers; they never fall back to another store. The descriptor cache may still use memory. The executable is `/bin/registry`, with default arguments `serve /etc/distribution/config.yml`. The configuration uses filesystem storage at `/var/lib/registry`, port 5000, in-memory descriptor caching and deletion support. Telemetry export is disabled by default. The health executable `/bin/axern-registry-healthcheck` verifies the local Distribution endpoint without a shell, proxy or redirect.

The image deliberately runs as numeric UID/GID `0:0` to preserve writes to retained root-owned Registry directories and blobs. Startup does not change ownership, migrate or delete existing data. Operators may supply another identity only after separately preparing compatible ownership for the entire retained store. A backup and actual push, pull, delete and restart qualification are required before adopting an existing store; source or image security checks do not prove storage migration compatibility.

The CA bundle is a dated, checksum-locked Mozilla snapshot. Update the bundle and its lock together. Distribution, Go, Axern and compiled dependency notices, plus the Mozilla MPL license, are installed under `/usr/share/licenses/axern-registry`.
