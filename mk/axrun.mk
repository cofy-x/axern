.PHONY: axrun-check axrun-verify axrun-acceptance-local axrun-local-smoke

axrun-verify: ## Run Axrun source checks and the complete local acceptance gate
	$(MAKE) axrun-check

axrun-check: ## Run the fast Axrun source and local TaskSet gates
	go test ./apps/axrun/... -count=1
	go vet ./apps/axrun/...
	test -z "$$(gofmt -l apps/axrun)"
	$(MAKE) axrun-local-smoke

axrun-acceptance-local: axrun-local-smoke ## Run local TaskSet acceptance

axrun-local-smoke: ## Verify deterministic local TaskSet compilation
	bash $(ROOTDIR)/scripts/axrun/local-smoke.sh
