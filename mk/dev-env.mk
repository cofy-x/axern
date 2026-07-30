.PHONY: quickstart quickstart-source axern-config-init \
		local-images-build nydus-builder-image registry-nydus-image-build \
		local-compose-up local-compose-down local-compose-status local-compose-purge local-compose-reset local-compose-refresh local-compose-refresh-verify local-compose-image-import local-compose-image-service-smoke local-compose-registry-image-smoke local-compose-image-mount-smoke local-compose-claude-code-image-mount-smoke local-compose-codex-image-mount-smoke local-compose-nydus-smoke \
		local-compose-smoke local-compose-doctor-smoke local-compose-gateway-smoke local-compose-gateway-ssh-e2e local-compose-service-volume-smoke local-compose-run-smoke local-compose-function-smoke local-compose-server-base-smoke local-compose-quota-smoke local-compose-tunnel-e2e local-compose-python-sdk-e2e local-compose-computer-use-e2e local-compose-go-sdk-e2e local-compose-managed-rollout-e2e tunnel-benchmark-compose \
		kind-up kind-down kind-status kind-purge kind-reset kind-refresh kind-refresh-verify registry-up registry-status registry-down registry-image-push kind-image-import kind-image-service-smoke kind-axern-registry-image-smoke kind-axern-nydus-smoke kind-smoke kind-gateway-smoke kind-service-volume-smoke kind-run-smoke kind-server-base-smoke kind-quota-smoke kind-tunnel-e2e kind-tunnel-relay-e2e kind-tunnel-multirelay-e2e kube-env-kind \
		local-refresh-verify local-truth-verify local-storage-verify \
		sdk-go-examples-smoke

quickstart: ## Start and verify Compose with the published Axern release
	bash $(ROOTDIR)/scripts/dev-env/quickstart-release.sh

quickstart-source: ## Build source images, then start and verify local Compose
	AXERN_IMAGE_MODE=source $(MAKE) local-compose-up
	AXERN_IMAGE_MODE=source $(MAKE) local-compose-smoke

axern-config-init: ## Refresh local axern contexts from repo-managed compose and kind state
	bash $(ROOTDIR)/scripts/dev-env/init-axern-config.sh

local-images-build: ## Build the shared local deploy images used by compose and kind
	bash $(ROOTDIR)/scripts/dev-env/build-images.sh

nydus-builder-image: ## Build the repo-managed Nydus conversion helper image
	bash $(ROOTDIR)/scripts/dev-env/build-nydus-builder-image.sh

registry-nydus-image-build: ## Build and push the repo-managed local Nydus smoke image
	bash $(ROOTDIR)/scripts/dev-env/registry-nydus-image-build.sh

local-compose-up: ## Bring up the local Docker Compose truth environment
	bash $(ROOTDIR)/scripts/dev-env/compose-up.sh

local-compose-down: ## Tear down the local Docker Compose truth environment
	bash $(ROOTDIR)/scripts/dev-env/compose-down.sh

local-compose-status: ## Show local Docker Compose truth environment status
	bash $(ROOTDIR)/scripts/dev-env/compose-status.sh

local-compose-image-import: ## Import IMAGE from host Docker into the local Docker Compose node image cache
	bash $(ROOTDIR)/scripts/dev-env/compose-image-import.sh

local-compose-image-service-smoke: ## Import python:3.12-slim into compose, then verify an axern service through gateway
	bash $(ROOTDIR)/scripts/dev-env/compose-image-service-smoke.sh

local-compose-registry-image-smoke: ## Verify Axern can start an image from the repo-managed local registry in compose
	bash $(ROOTDIR)/scripts/dev-env/compose-registry-image-smoke.sh

local-compose-image-mount-smoke: ## Verify compose run image_mounts with a read-only reusable image bundle
	bash $(ROOTDIR)/scripts/dev-env/compose-image-mount-smoke.sh

local-compose-claude-code-image-mount-smoke: ## Verify Claude Code as a read-only image mount bundle in a task sandbox
	bash $(ROOTDIR)/scripts/dev-env/compose-claude-code-image-mount-smoke.sh

local-compose-codex-image-mount-smoke: ## Verify Codex as a read-only image mount bundle in a task sandbox
	bash $(ROOTDIR)/scripts/dev-env/compose-codex-image-mount-smoke.sh

local-compose-nydus-smoke: ## Verify Axern can start a compose sandbox from a Nydus image through imagemgr/imagefsd
	bash $(ROOTDIR)/scripts/dev-env/compose-nydus-smoke.sh

local-compose-purge: ## Remove the local Docker Compose truth environment and its repo-local state
	bash $(ROOTDIR)/scripts/dev-env/compose-purge.sh

local-compose-reset: ## Recreate the local Docker Compose truth environment from a clean repo-local state
	bash $(ROOTDIR)/scripts/dev-env/compose-reset.sh

local-compose-refresh: ## Refresh compose images, reset Postgres/MinIO state, and redeploy without purging all state
	bash $(ROOTDIR)/scripts/dev-env/compose-refresh.sh

local-compose-refresh-verify: ## Run the lightweight compose refresh path and core compose smoke suite
	bash $(ROOTDIR)/scripts/dev-env/verify-compose-refresh.sh

local-compose-smoke: ## Run the local Docker Compose truth-environment smoke contract
	bash $(ROOTDIR)/scripts/dev-env/compose-smoke.sh

local-compose-doctor-smoke: ## Verify platform doctor read and data-plane probe paths in Compose
	bash $(ROOTDIR)/scripts/dev-env/compose-doctor-smoke.sh

local-compose-gateway-smoke: ## Run the local Docker Compose gateway HTTP and terminal smoke contract
	bash $(ROOTDIR)/scripts/dev-env/compose-gateway-smoke.sh

local-compose-gateway-ssh-e2e: local-compose-up ## Verify gateway SSH terminal with a real ssh client in compose
	bash $(ROOTDIR)/scripts/dev-env/compose-gateway-smoke.sh

local-compose-service-volume-smoke: ## Run the local Docker Compose service volume truth-path smoke
	bash $(ROOTDIR)/scripts/dev-env/compose-service-volume-smoke.sh

local-compose-run-smoke: ## Run the local Docker Compose run truth-path smoke
	bash $(ROOTDIR)/scripts/dev-env/compose-run-smoke.sh

local-compose-function-smoke: ## Run the local Docker Compose Function deploy and invoke smoke
	bash $(ROOTDIR)/scripts/dev-env/compose-function-smoke.sh

local-compose-server-base-smoke: ## Run the local Docker Compose server-base default-entrypoint smoke
	bash $(ROOTDIR)/scripts/dev-env/compose-server-base-smoke.sh

local-compose-quota-smoke: ## Run the local Docker Compose quota admission smoke
	bash $(ROOTDIR)/scripts/dev-env/compose-quota-smoke.sh

local-compose-tunnel-e2e: ## Verify Axern tunnel end-to-end in compose for runsc and runc
	bash $(ROOTDIR)/scripts/dev-env/compose-tunnel-e2e.sh

local-compose-python-sdk-e2e: ## Verify the Python SDK Sandbox tunnel flow in compose
	bash $(ROOTDIR)/scripts/dev-env/compose-python-sdk-e2e.sh

local-compose-computer-use-e2e: ## Verify desktop-base sandboxd desktop APIs in compose
	bash $(ROOTDIR)/scripts/dev-env/compose-computer-use-e2e.sh

local-compose-go-sdk-e2e: ## Verify the Go SDK programmable Sandbox flow in compose
	bash $(ROOTDIR)/scripts/dev-env/compose-go-sdk-e2e.sh

local-compose-managed-rollout-e2e: local-compose-up ## Verify managed rollout with the local-only deterministic provider
	bash $(ROOTDIR)/scripts/dev-env/compose-managed-rollout-e2e.sh

tunnel-benchmark-compose: ## Record tunnel performance baseline in the compose truth environment
	bash $(ROOTDIR)/scripts/dev-env/compose-tunnel-benchmark.sh

kind-up: ## Bring up the repo-managed kind truth environment
	bash $(ROOTDIR)/scripts/dev-env/kind-up.sh

kind-down: ## Tear down the repo-managed kind truth environment
	bash $(ROOTDIR)/scripts/dev-env/kind-down.sh

kind-status: ## Show repo-managed kind truth environment status
	bash $(ROOTDIR)/scripts/dev-env/kind-status.sh

registry-up: ## Start the Docker-backed repo-managed local registry
	bash $(ROOTDIR)/scripts/dev-env/registry-up.sh

registry-status: ## Show repo-managed local registry status
	bash $(ROOTDIR)/scripts/dev-env/registry-status.sh

registry-down: ## Stop the Docker-backed repo-managed local registry
	bash $(ROOTDIR)/scripts/dev-env/registry-down.sh

registry-image-push: ## Push IMAGE into the repo-managed local registry
	bash $(ROOTDIR)/scripts/dev-env/registry-image-push.sh

kind-image-import: ## Import IMAGE from host Docker into every repo-managed kind node image cache
	bash $(ROOTDIR)/scripts/dev-env/kind-image-import.sh

kind-image-service-smoke: ## Import python:3.12-slim into kind nodes, then verify an axern service through gateway
	bash $(ROOTDIR)/scripts/dev-env/kind-image-service-smoke.sh

kind-axern-registry-image-smoke: ## Verify Axern can start an image from the repo-managed local registry in kind
	bash $(ROOTDIR)/scripts/dev-env/kind-axern-registry-image-smoke.sh

kind-axern-nydus-smoke: ## Verify Axern can start a sandbox from a Nydus image through imagemgr/imagefsd
	bash $(ROOTDIR)/scripts/dev-env/kind-axern-nydus-smoke.sh

kind-purge: ## Remove the repo-managed kind truth environment and its repo-local state
	bash $(ROOTDIR)/scripts/dev-env/kind-purge.sh

kind-reset: ## Recreate the repo-managed kind truth environment from a clean repo-local state
	bash $(ROOTDIR)/scripts/dev-env/kind-reset.sh

kind-refresh: ## Refresh kind core images, reset Postgres, and redeploy without deleting the cluster
	bash $(ROOTDIR)/scripts/dev-env/kind-refresh.sh

kind-refresh-verify: ## Run the lightweight kind refresh path and core kind smoke suite
	bash $(ROOTDIR)/scripts/dev-env/verify-kind-refresh.sh

kind-smoke: ## Run the repo-managed kind truth-environment smoke contract
	bash $(ROOTDIR)/scripts/dev-env/kind-smoke.sh

kind-gateway-smoke: ## Run the repo-managed kind gateway HTTP and terminal smoke contract
	bash $(ROOTDIR)/scripts/dev-env/kind-gateway-smoke.sh

kind-service-volume-smoke: ## Run the repo-managed kind service volume truth-path smoke
	bash $(ROOTDIR)/scripts/dev-env/kind-service-volume-smoke.sh

kind-run-smoke: ## Run the repo-managed kind run truth-path smoke
	bash $(ROOTDIR)/scripts/dev-env/kind-run-smoke.sh

kind-server-base-smoke: ## Run the repo-managed kind server-base default-entrypoint smoke
	bash $(ROOTDIR)/scripts/dev-env/kind-server-base-smoke.sh

kind-quota-smoke: ## Run the repo-managed kind quota admission smoke
	bash $(ROOTDIR)/scripts/dev-env/kind-quota-smoke.sh

kind-tunnel-e2e: ## Verify Axern tunnel end-to-end in kind for runsc and runc
	bash $(ROOTDIR)/scripts/dev-env/kind-tunnel-e2e.sh

kind-tunnel-relay-e2e: ## Verify kind tunnel relay registry, drain selection, and peer events
	bash $(ROOTDIR)/scripts/dev-env/kind-tunnel-relay-e2e.sh

kind-tunnel-multirelay-e2e: ## Verify kind tunnel behavior with two physical relay deployments
	bash $(ROOTDIR)/scripts/dev-env/kind-tunnel-multirelay-e2e.sh

kube-env-kind: ## Print shell exports/functions for the repo-managed kind cluster
	bash $(ROOTDIR)/scripts/dev-env/kube-env-kind.sh

local-truth-verify: ## Reset kind and compose, then run the full local truth-environment smoke suite
	bash $(ROOTDIR)/scripts/dev-env/verify-local-truth.sh

local-refresh-verify: ## Refresh compose and kind without deleting environments, then run core smoke suites
	bash $(ROOTDIR)/scripts/dev-env/verify-local-refresh.sh

local-storage-verify: ## Run compose and kind service-volume truth-path smokes without resetting environments
	bash $(ROOTDIR)/scripts/dev-env/verify-local-storage.sh

sdk-go-examples-smoke: ## Run lightweight Go SDK examples against local compose
	bash $(ROOTDIR)/scripts/dev-env/go-sdk-examples-smoke.sh
