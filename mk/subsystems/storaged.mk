STORAGED_DIR := control/storaged

.PHONY: \
	storaged-help \
	storaged-build \
	storaged-test \
	storaged-fmt \
	storaged-vet

storaged-help: ## Show storaged targets
	@$(call run_subsystem_make,$(STORAGED_DIR),help)

storaged-build: ## Build the storaged daemon
	@$(call run_subsystem_make,$(STORAGED_DIR),build)

storaged-test: ## Run storaged tests
	@$(call run_subsystem_make,$(STORAGED_DIR),test)

storaged-fmt: ## Format storaged Go code
	@$(call run_subsystem_make,$(STORAGED_DIR),fmt)

storaged-vet: ## Run storaged go vet checks
	@$(call run_subsystem_make,$(STORAGED_DIR),vet)
