CONTROLD_DIR := control/controld

.PHONY: \
	controld-help \
	controld-build \
	controld-test \
	controld-fmt \
	controld-vet

controld-help: ## Show controld targets
	@$(call run_subsystem_make,$(CONTROLD_DIR),help)

controld-build: ## Build the controld daemon
	@$(call run_subsystem_make,$(CONTROLD_DIR),build)

controld-test: ## Run controld tests
	@$(call run_subsystem_make,$(CONTROLD_DIR),test)

controld-fmt: ## Format controld Go code
	@$(call run_subsystem_make,$(CONTROLD_DIR),fmt)

controld-vet: ## Run controld go vet checks
	@$(call run_subsystem_make,$(CONTROLD_DIR),vet)
