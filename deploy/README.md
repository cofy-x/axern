# Deploy

Cloud-neutral packaging and deployment assets owned by Axern live here.

The product direction prioritizes Linux single-host serving with gVisor (`runsc`), while macOS supports clients and VM-backed local development. [Single-Host Agent Serving](../docs/decisions/single-host-agent-serving.md) defines the planned deployment simplification. The assets below retain their current roles: local Compose and kind are development and verification environments, and Helm is the existing Kubernetes deployment surface. A dedicated Linux serving package remains planned.

Local surface:

- `deploy/local/compose/` for the Docker Compose truth environment
- `deploy/local/kind/` for the repo-managed kind cluster definition
- `deploy/local/k8s/` for the shared Kubernetes manifests used by the repo-managed kind environment
- `deploy/local/state/` for generated local PKI, CLI bootstrap, and runtime state

Image packaging lives under `deploy/images/`.

The local deploy image flow uses a shared node-runtime base image under `deploy/images/lib/`, then produces separate final images for:

- local deployment (`axern/local-node-all-in-one:dev`)
- verification (`axnoded-verify:latest`)

`deploy/` owns image packaging, the local truth environment, and the generic Kubernetes Helm chart. Provider-specific values, credentials, cluster paths, and cloud resource orchestration live outside this repository. For local environment usage, smoke entrypoints, cleanup commands, and CLI bootstrap, use [Local Deployment](./local/README.md).
