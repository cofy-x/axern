.PHONY: axrun-check axrun-verify axrun-acceptance-local axrun-local-smoke axrun-managed-rollout-compose-e2e

axrun-verify: ## Run Axrun source checks and the complete local acceptance gate
	$(MAKE) axrun-check
	$(MAKE) axrun-managed-rollout-compose-e2e

axrun-check: ## Run the fast Axrun source and local TaskSet gates
	go test ./apps/axrun/... -count=1
	go vet ./apps/axrun/...
	test -z "$$(gofmt -l apps/axrun)"
	$(MAKE) axrun-local-smoke

axrun-acceptance-local: axrun-local-smoke axrun-managed-rollout-compose-e2e ## Run local TaskSet and managed rollout acceptance

axrun-local-smoke: ## Verify deterministic local TaskSet compilation
	bash $(ROOTDIR)/scripts/axrun/local-smoke.sh

axrun-managed-rollout-compose-e2e: ## Run the mandatory managed rollout mock-provider Compose gate
	OTEL=0 AXERN_LOCAL_IMAGE_SCOPE=managed-rollout AXERN_SKIP_COMPOSE_RUNTIME_IMAGE_IMPORTS=1 $(MAKE) local-compose-up
	@if [ "$${AXERN_MANAGED_ROLLOUT_PRUNE_BUILD_CACHE:-0}" = "1" ] || [ "$${AXERN_MANAGED_ROLLOUT_PRUNE_BUILD_CACHE:-0}" = "true" ]; then \
		docker builder prune --all --force; \
	fi
	bash $(ROOTDIR)/scripts/dev-env/compose-managed-rollout-e2e.sh
