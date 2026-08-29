EGRESSD_DIR := runtime/egressd

.PHONY: \
	egressd-help \
	egressd-build \
	egressd-test \
	egressd-verify \
	egressd-linux-truth \
	egressd-fmt \
	egressd-vet \
	egressd-tidy

egressd-help: ## Show egressd targets
	@$(call run_subsystem_make,$(EGRESSD_DIR),help)

egressd-build: ## Build egressd daemon
	@$(call run_subsystem_make,$(EGRESSD_DIR),build)

egressd-test: ## Run egressd tests
	@$(call run_subsystem_make,$(EGRESSD_DIR),test)

egressd-verify: ## Run egressd tests, race checks, and vet
	@$(call run_subsystem_make,$(EGRESSD_DIR),verify)

egressd-fmt: ## Format egressd Go code
	@$(call run_subsystem_make,$(EGRESSD_DIR),fmt)

egressd-vet: ## Run egressd go vet checks
	@$(call run_subsystem_make,$(EGRESSD_DIR),vet)

egressd-tidy: ## Ensure egressd go.mod and go.sum are up to date
	@$(call run_subsystem_make,$(EGRESSD_DIR),tidy)

egressd-linux-truth: egressd-build ## Run privileged Linux network-namespace policy truth tests
	@$(EGRESSD_DIR)/scripts/linux-truth.sh
