AXNODED_DIR := runtime/axnoded

.PHONY: \
	axnoded-help \
	axnoded-build \
	axnoded-release \
	axnoded-release-binary \
	axnoded-release-cli \
	axnoded-test \
	axnoded-check-architecture \
	axnoded-fmt \
	axnoded-vet \
	axnoded-clean \
	axnoded-protos \
	axnoded-protos-docker \
	axnoded-tidy \
	axnoded-vendor \
	axnoded-verify-docker-build \
	axnoded-verify-sandboxd-e2e \
	axnoded-verify-sandboxd-oci-e2e \
	axnoded-verify-docker \
	axnoded-verify-node-inventory-e2e \
	axnoded-verify-node-startup-metrics-e2e \
	axnoded-verify-node-startup-matrix-smoke \
	axnoded-verify-node-bundle-template-e2e \
	axnoded-verify-node-python-runtime-e2e \
	axnoded-verify-node-retention-e2e \
	axnoded-verify-node-locality-e2e \
	axnoded-verify-node-warm-pool-e2e \
	axnoded-verify-node-oci-e2e \
	axnoded-verify-node-nydus-e2e \
	axnoded-verify-node-oss-e2e \
	axnoded-build-python311-runtime-image \
	axnoded-build-server-base-runtime-image \
	axnoded-build-coding-base-runtime-image \
	axnoded-build-claude-code-bundle-image \
	axnoded-build-codex-bundle-image \
	axnoded-verify-docker-runsc \
	axnoded-verify-docker-runsc-debug \
	axnoded-verify-docker-runc \
	axnoded-verify-docker-runc-debug \
	axnoded-verify-docker-conformance \
	axnoded-benchmark-startup-matrix \
	axnoded-run-nginx-demo \
	axnoded-stop-nginx-demo \
	axnoded-run-dashboard-nginx-demo \
	axnoded-stop-dashboard-nginx-demo

axnoded-help: ## Show axnoded targets
	@$(call run_subsystem_make,$(AXNODED_DIR),help)

axnoded-build: ## Build axnoded daemon and CLI
	@$(call run_subsystem_make,$(AXNODED_DIR),release)

axnoded-release: ## Build axnoded daemon and CLI
	@$(call run_subsystem_make,$(AXNODED_DIR),release)

axnoded-release-binary: ## Build the axnoded daemon binary
	@$(call run_subsystem_make,$(AXNODED_DIR),release-binary)

axnoded-release-cli: ## Build the axnoded CLI binary
	@$(call run_subsystem_make,$(AXNODED_DIR),release-cli)

axnoded-test: ## Run axnoded tests
	@$(call run_subsystem_make,$(AXNODED_DIR),test)

axnoded-check-architecture: ## Verify axnoded package boundary constraints
	@$(call run_subsystem_make,$(AXNODED_DIR),check-architecture)

axnoded-fmt: ## Format axnoded Go code
	@$(call run_subsystem_make,$(AXNODED_DIR),fmt)

axnoded-vet: ## Run axnoded go vet checks
	@$(call run_subsystem_make,$(AXNODED_DIR),vet)

axnoded-clean: ## Clean axnoded build artifacts
	@$(call run_subsystem_make,$(AXNODED_DIR),clean)

axnoded-protos: ## Regenerate axnoded protobuf code
	@$(call run_subsystem_make,$(AXNODED_DIR),protos)

axnoded-protos-docker: ## Regenerate axnoded protobuf code via Docker
	@$(call run_subsystem_make,$(AXNODED_DIR),protos-docker)

axnoded-tidy: ## Ensure axnoded go.mod and go.sum are up to date
	@$(call run_subsystem_make,$(AXNODED_DIR),tidy)

axnoded-vendor: ## Sync the axnoded vendor directory
	@$(call run_subsystem_make,$(AXNODED_DIR),vendor)

axnoded-verify-docker-build: ## Build the axnoded privileged Docker verification image
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-docker-build)

axnoded-verify-docker: ## Run the axnoded privileged Docker verification matrix
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-docker)

axnoded-verify-sandboxd-e2e: ## Run the focused axern-sandboxd Docker e2e
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-sandboxd-e2e)

axnoded-verify-sandboxd-oci-e2e: ## Run the focused axern-sandboxd OCI injection e2e
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-sandboxd-oci-e2e)

axnoded-verify-node-inventory-e2e: ## Run the axnoded node all-in-one inventory end-to-end verification
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-node-inventory-e2e)

axnoded-verify-node-startup-metrics-e2e: ## Run the axnoded node all-in-one startup metrics end-to-end verification
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-node-startup-metrics-e2e)

axnoded-verify-node-startup-matrix-smoke: ## Run the axnoded startup matrix smoke verification
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-node-startup-matrix-smoke)

axnoded-verify-node-bundle-template-e2e: ## Run the axnoded node all-in-one bundle template end-to-end verification
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-node-bundle-template-e2e)

axnoded-verify-node-python-runtime-e2e: ## Run the axnoded node all-in-one programmable Python runtime verification
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-node-python-runtime-e2e)

axnoded-verify-node-retention-e2e: ## Run the axnoded node all-in-one runtime retention end-to-end verification
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-node-retention-e2e)

axnoded-verify-node-locality-e2e: ## Run the axnoded node all-in-one locality signals end-to-end verification
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-node-locality-e2e)

axnoded-verify-node-warm-pool-e2e: ## Run the axnoded node all-in-one warm pool end-to-end verification
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-node-warm-pool-e2e)

axnoded-verify-node-oci-e2e: ## Run the axnoded node all-in-one OCI end-to-end verification
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-node-oci-e2e)

axnoded-verify-node-nydus-e2e: ## Run the axnoded node all-in-one Nydus end-to-end verification
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-node-nydus-e2e)

axnoded-verify-node-oss-e2e: ## Run the axnoded node all-in-one OSS end-to-end verification
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-node-oss-e2e)

axnoded-build-python311-runtime-image: ## Build the official axnoded Python 3.11 runtime image
	@$(call run_subsystem_make,$(AXNODED_DIR),build-python311-runtime-image)

axnoded-build-server-base-runtime-image: ## Build the official axnoded server-base runtime image
	@$(call run_subsystem_make,$(AXNODED_DIR),build-server-base-runtime-image)

axnoded-build-coding-base-runtime-image: ## Build the official axnoded coding-base runtime image
	@$(call run_subsystem_make,$(AXNODED_DIR),build-coding-base-runtime-image)

axnoded-build-claude-code-bundle-image: ## Build the axnoded Claude Code image mount bundle
	@$(call run_subsystem_make,$(AXNODED_DIR),build-claude-code-bundle-image)

axnoded-build-codex-bundle-image: ## Build the axnoded Codex image mount bundle
	@$(call run_subsystem_make,$(AXNODED_DIR),build-codex-bundle-image)

axnoded-verify-docker-runsc: ## Run axnoded privileged Docker verification against runsc
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-docker-runsc)

axnoded-verify-docker-runsc-debug: ## Run axnoded privileged Docker verification against runsc with diagnostics
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-docker-runsc-debug)

axnoded-verify-docker-runc: ## Run axnoded privileged Docker verification against runc
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-docker-runc)

axnoded-verify-docker-runc-debug: ## Run axnoded privileged Docker verification against runc with diagnostics
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-docker-runc-debug)

axnoded-verify-docker-conformance: ## Run production cgroup and serialized runtime certification truth
	@$(call run_subsystem_make,$(AXNODED_DIR),verify-docker-conformance)

axnoded-benchmark-startup-matrix: ## Run the axnoded startup quantile matrix benchmark
	@$(call run_subsystem_make,$(AXNODED_DIR),benchmark-startup-matrix)

axnoded-run-nginx-demo: ## Run the axnoded nginx demo
	@$(call run_subsystem_make,$(AXNODED_DIR),run-nginx-demo)

axnoded-stop-nginx-demo: ## Stop the axnoded nginx demo
	@$(call run_subsystem_make,$(AXNODED_DIR),stop-nginx-demo)

axnoded-run-dashboard-nginx-demo: ## Run the axnoded dashboard nginx demo
	@$(call run_subsystem_make,$(AXNODED_DIR),run-dashboard-nginx-demo)

axnoded-stop-dashboard-nginx-demo: ## Stop the axnoded dashboard nginx demo
	@$(call run_subsystem_make,$(AXNODED_DIR),stop-dashboard-nginx-demo)
