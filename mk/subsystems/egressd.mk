EGRESSD_DIR := runtime/egressd

.PHONY: \
	egressd-help \
	egressd-build \
	egressd-test \
	egressd-verify \
	network-policy-fuzz-smoke \
	network-policy-qualification-contract \
	network-policy-qualification \
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

network-policy-fuzz-smoke: ## Run bounded fuzz smoke for policy normalization and trusted egress parsers
	@$(GO) -C lib/go/networkpolicy test -run='^$$' -fuzz='^FuzzNormalizeDomain$$' -fuzztime=3s -parallel=1 .
	@$(GO) -C lib/go/networkpolicy test -run='^$$' -fuzz='^FuzzNormalizePolicy$$' -fuzztime=3s -parallel=1 .
	@$(call run_subsystem_make,$(EGRESSD_DIR),fuzz-smoke)

network-policy-qualification-contract: ## Validate network-policy qualification schemas and relative budgets
	@$(call run_subsystem_make,$(EGRESSD_DIR),qualification-contract)

network-policy-qualification: ## Run the full Linux network-policy qualification matrix
	@$(call run_subsystem_make,$(EGRESSD_DIR),qualification)

egressd-fmt: ## Format egressd Go code
	@$(call run_subsystem_make,$(EGRESSD_DIR),fmt)

egressd-vet: ## Run egressd go vet checks
	@$(call run_subsystem_make,$(EGRESSD_DIR),vet)

egressd-tidy: ## Ensure egressd go.mod and go.sum are up to date
	@$(call run_subsystem_make,$(EGRESSD_DIR),tidy)

egressd-linux-truth: egressd-build ## Run privileged Linux network-namespace policy truth tests
	@$(EGRESSD_DIR)/scripts/linux-truth.sh
