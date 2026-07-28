VOLUMED_DIR := runtime/volumed

.PHONY: \
	volumed-help \
	volumed-build \
	volumed-test \
	volumed-fmt \
	volumed-vet \
	volumed-tidy

volumed-help: ## Show volumed targets
	@$(call run_subsystem_make,$(VOLUMED_DIR),help)

volumed-build: ## Build volumed daemon
	@$(call run_subsystem_make,$(VOLUMED_DIR),build)

volumed-test: ## Run volumed tests
	@$(call run_subsystem_make,$(VOLUMED_DIR),test)

volumed-fmt: ## Format volumed Go code
	@$(call run_subsystem_make,$(VOLUMED_DIR),fmt)

volumed-vet: ## Run volumed go vet checks
	@$(call run_subsystem_make,$(VOLUMED_DIR),vet)

volumed-tidy: ## Ensure volumed go.mod and go.sum are up to date
	@$(call run_subsystem_make,$(VOLUMED_DIR),tidy)
