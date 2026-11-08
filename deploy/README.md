# Deploy

Cloud-neutral packaging and deployment assets owned by Axern live here.

The product direction prioritizes Linux single-host serving with gVisor (`runsc`), while macOS supports clients and VM-backed local development. [Single-Host Agent Serving](../docs/decisions/single-host-agent-serving.md) defines the deployment direction. [Native Linux deployment](standalone/README.md) provides candidate runtime packaging and experimental systemd installation. Compose and kind are development and verification environments; Helm is the Kubernetes deployment surface. The native candidate is not a qualified durable serving profile.

Local surface:

- `deploy/standalone/` for native runtime packaging and systemd installation
- `deploy/local/compose/` for the Docker Compose truth environment
- `deploy/local/kind/` for the repo-managed kind cluster definition
- `deploy/local/k8s/` for the shared Kubernetes manifests used by the repo-managed kind environment
- `deploy/local/state/` for generated local PKI, CLI bootstrap, and runtime state

Image packaging lives under `deploy/images/`.

The coordinated development delivery includes the four Axern service images plus PostgreSQL and Registry images. Their pinned upstream source, toolchain and package refresh recipes live under `deploy/images/postgres` and `deploy/images/registry`; the release lock binds every final image to immutable bytes. Native installation continues to use its documented host PostgreSQL requirement.

Serving images contain the Axern services, selected gVisor runtime and host utilities needed for execution and enforcement. The node image excludes the Go SDK, Git and kernel analysis tools. Development tools belong in verification images; tools used by Agents belong in their [workload images](../images/agents/README.md).

The local deploy image flow uses a shared node-runtime base image under `deploy/images/lib/`, then produces separate final images for:

- local deployment (`axern/local-node-all-in-one:dev`)
- verification (`axnoded-verify:latest`)

`deploy/` owns image packaging, the local truth environment, and the generic Kubernetes Helm chart. Provider-specific values, credentials, cluster paths, and cloud resource orchestration live outside this repository. For local environment usage, smoke entrypoints, cleanup commands, and CLI bootstrap, use [Local Deployment](./local/README.md).
