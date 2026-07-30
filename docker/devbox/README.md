# Axern Devbox

The Axern devbox image is the standalone Linux workspace for node-runtime
development. It is built from a public Ubuntu base image and started by the
repo-local [devbox wrapper](../../scripts/devbox/devbox.sh).

It includes:

- Go `1.25.12`
- `GOPROXY=https://proxy.golang.org,direct` for the `axern` devbox user
- Rust `1.89.0` with `rustfmt`
- Postgres for the standalone source-development stack
- Node.js, pnpm, Python, and uv for root workspace development
- `runsc` and `runc` for runtime validation
- Docker CLI plus `buildx` and `compose` plugins for the mounted host Docker socket
- network, filesystem, FUSE, and debug tools used by the node verification flow
- `sshd` managed by `supervisord` for VS Code Remote-SSH

Build it with:

```bash
make devbox-image-build
```

Image builds use upstream Ubuntu and language package sources by default. No
host build proxy is enabled automatically.

Regional mirrors and a host proxy remain explicit options:

```bash
DEVBOX_APT_MIRROR_SOURCE=aliyun \
DEVBOX_BUILD_PROXY=http://host.docker.internal:8080 \
GOPROXY=https://goproxy.cn,direct \
NPM_CONFIG_REGISTRY=https://registry.npmmirror.com \
make devbox-image-build
```

Start the project devbox with:

```bash
make devbox-up
```

See [Devbox Workflow](../../docs/operations/devbox.md) for the full standalone
development workflow, including stack startup, per-service restarts, VS Code
Remote-SSH debugging, and verification commands.

The default SSH endpoint is written to `~/.ssh/config` as
`Host axern-devbox`, so VS Code Remote-SSH can attach to `axern-devbox`.
Toolchain commands are available through the default SSH `PATH`, and the
mounted workspace `bin/` directory is added at container startup for
repo-local CLI builds.

Override the port with:

```bash
DEVBOX_SSH_PORT=23022 make devbox-up
```

Override the SSH alias with:

```bash
DEVBOX_SSH_CONFIG_HOST=axern-devbox-arm64 make devbox-up
```

Enter a running devbox without SSH using:

```bash
make devbox-shell
```

Run the recommended verification flow from inside the devbox with:

```bash
make axnoded-verify-docker
```
