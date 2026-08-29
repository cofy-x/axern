.PHONY: node-dev-prepare node-dev-clean \
		node-dev-ensure-dlv axnoded-debug-server imagemgr-debug-server \
		postgres-dev-up postgres-dev-down storaged-dev-run controld-dev-prepare controld-dev-run gatewayd-dev-run \
		axern-dev axern-dev-build axctl-dev axctl-dev-build egressd-dev-run \
		dev-runtime-images-build dev-runtime-images-load \
		axnoded-dev-run imagemgr-dev-run volumed-dev-run imagefsd-dev-serve-chunk \
		dev-stack-up dev-stack-status dev-stack-down dev-stack-restart dev-stack-logs dev-stack-reset \
		devbox-image-build devbox-up devbox-status devbox-down devbox-shell \
		devbox-stack-up devbox-stack-status devbox-stack-down devbox-stack-restart devbox-stack-logs devbox-stack-reset \
		devbox-runtime-images-load devbox-axern devbox-axctl

NODE_DEV_DIR := $(ROOTDIR)/.dev
NODE_DEV_RUN_DIR := $(NODE_DEV_DIR)/run
AXNODED_DEV_DIR := $(NODE_DEV_DIR)/axnoded
IMAGEMGR_DEV_DIR := $(NODE_DEV_DIR)/imagemgr
VOLUMED_DEV_DIR := $(NODE_DEV_DIR)/volumed
EGRESSD_DEV_DIR := $(NODE_DEV_DIR)/egressd
IMAGEFSD_DEV_DIR := $(NODE_DEV_DIR)/imagefsd
AXNODED_DEV_DAP_PORT ?= 43001
IMAGEMGR_DEV_DAP_PORT ?= 43002
AXERN_DEV_TOKEN ?= axern-local-dev
AXERN_SECRETS_MASTER_KEY ?= local-only-master-key-32-bytes!!
POSTGRES_DSN ?= postgres://postgres:postgres@127.0.0.1:5432/axern?sslmode=disable
AXERN_DEV_CONTROL_TARGET ?= 127.0.0.1:24000
AXCTL_DEV_ADDRESS ?= $(NODE_DEV_RUN_DIR)/axnoded.sock
DEV_RUNTIME_IMAGES ?= python311

define ensure_linux_workspace
	@if [ "$$(uname -s)" != "Linux" ]; then \
		echo "This target requires a Linux workspace. Run it inside 'make devbox-up'."; \
		exit 1; \
	fi
endef

node-dev-prepare: ## Prepare the repo-local Linux runtime dev workspace under .dev
	$(call ensure_linux_workspace)
	bash $(ROOTDIR)/scripts/devbox/node-dev-prepare.sh

node-dev-clean: ## Remove the repo-local Linux runtime dev workspace
	@if [ -d "$(NODE_DEV_DIR)" ]; then \
		if command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then \
			sudo rm -rf "$(NODE_DEV_DIR)"; \
		else \
			rm -rf "$(NODE_DEV_DIR)"; \
		fi; \
	fi

node-dev-ensure-dlv:
	$(call ensure_linux_workspace)
	@bash $(ROOTDIR)/scripts/devbox/node-dev-ensure-dlv.sh '$(NODE_DEV_DIR)/dlv-install.lock'

axnoded-debug-server: node-dev-prepare node-dev-ensure-dlv
	$(call ensure_linux_workspace)
	@bash -lc 'set -euo pipefail; \
		port="$(AXNODED_DEV_DAP_PORT)"; \
		log_file="$(AXNODED_DEV_DIR)/logs/dlv-dap.log"; \
		if ss -ltn "( sport = :$$port )" | tail -n +2 | grep -q .; then exit 0; fi; \
		dlv_bin="$$(command -v dlv || true)"; \
		if [ -z "$$dlv_bin" ] && [ -x "$$HOME/.local/bin/dlv" ]; then dlv_bin="$$HOME/.local/bin/dlv"; fi; \
		if [ -z "$$dlv_bin" ]; then echo "dlv not found after installation step" >&2; exit 1; fi; \
		if [ "$$(id -u)" -eq 0 ]; then \
			nohup "$$dlv_bin" dap --listen=127.0.0.1:$$port >"$$log_file" 2>&1 & \
		elif command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then \
			sudo -n bash -lc "nohup '\''$$dlv_bin'\'' dap --listen=127.0.0.1:$$port > '\''$$log_file'\'' 2>&1 &"; \
		else \
			echo "axnoded debugging requires passwordless sudo inside the Linux workspace." >&2; \
			exit 1; \
		fi; \
		for _ in $$(seq 1 20); do \
			if ss -ltn "( sport = :$$port )" | tail -n +2 | grep -q .; then exit 0; fi; \
			sleep 0.2; \
		done; \
		echo "axnoded Delve DAP did not start on port $$port" >&2; \
		test -f "$$log_file" && cat "$$log_file" >&2; \
		exit 1'

imagemgr-debug-server: node-dev-prepare node-dev-ensure-dlv
	$(call ensure_linux_workspace)
	@bash -lc 'set -euo pipefail; \
		port="$(IMAGEMGR_DEV_DAP_PORT)"; \
		log_file="$(IMAGEMGR_DEV_DIR)/dlv-dap.log"; \
		if ss -ltn "( sport = :$$port )" | tail -n +2 | grep -q .; then exit 0; fi; \
		dlv_bin="$$(command -v dlv || true)"; \
		if [ -z "$$dlv_bin" ] && [ -x "$$HOME/.local/bin/dlv" ]; then dlv_bin="$$HOME/.local/bin/dlv"; fi; \
		if [ -z "$$dlv_bin" ]; then echo "dlv not found after installation step" >&2; exit 1; fi; \
		if [ "$$(id -u)" -eq 0 ]; then \
			nohup "$$dlv_bin" dap --listen=127.0.0.1:$$port >"$$log_file" 2>&1 & \
		elif command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then \
			sudo -n bash -lc "nohup '\''$$dlv_bin'\'' dap --listen=127.0.0.1:$$port > '\''$$log_file'\'' 2>&1 &"; \
		else \
			echo "imagemgr debugging requires passwordless sudo inside the Linux workspace." >&2; \
			exit 1; \
		fi; \
		for _ in $$(seq 1 20); do \
			if ss -ltn "( sport = :$$port )" | tail -n +2 | grep -q .; then exit 0; fi; \
			sleep 0.2; \
		done; \
		echo "imagemgr Delve DAP did not start on port $$port" >&2; \
		test -f "$$log_file" && cat "$$log_file" >&2; \
		exit 1'

postgres-dev-up: ## Start repo-local Postgres for manual dev services
	$(call ensure_linux_workspace)
	bash $(ROOTDIR)/scripts/devbox/stack.sh postgres-up

postgres-dev-down: ## Stop repo-local Postgres used by manual dev services
	$(call ensure_linux_workspace)
	bash $(ROOTDIR)/scripts/devbox/stack.sh postgres-down

controld-dev-run: node-dev-prepare postgres-dev-up ## Run controld in the repo-local Linux dev workspace
	$(call ensure_linux_workspace)
	bash $(ROOTDIR)/scripts/devbox/stack.sh migrate
	exec $(GO) -C $(ROOTDIR)/control/controld run ./cmd/controld \
		-grpc-address 127.0.0.1:24000 \
		-http-address 127.0.0.1:24001 \
		-heartbeat-freshness-window 15s \
		-summary-freshness-window 15s \
		-tls-ca-cert '$(NODE_DEV_DIR)/certs/ca.crt' \
		-tls-cert '$(NODE_DEV_DIR)/certs/controld.crt' \
		-tls-key '$(NODE_DEV_DIR)/certs/controld.key' \
		-secrets-master-key '$(AXERN_SECRETS_MASTER_KEY)' \
		-storaged-target '127.0.0.1:24020' \
		-tunnel-relays 'default,127.0.0.1:25000,127.0.0.1:24100,1,false' \
		-postgres-dsn '$(POSTGRES_DSN)'

controld-dev-prepare: node-dev-prepare postgres-dev-up ## Prepare Postgres and migrations for controld debugging
	$(call ensure_linux_workspace)
	bash $(ROOTDIR)/scripts/devbox/stack.sh migrate

storaged-dev-run: node-dev-prepare postgres-dev-up ## Run storaged in the repo-local Linux dev workspace
	$(call ensure_linux_workspace)
	exec $(GO) -C $(ROOTDIR)/control/storaged run ./cmd/storaged \
		-grpc-address 127.0.0.1:24020 \
		-http-address 127.0.0.1:24021 \
		-postgres-dsn '$(POSTGRES_DSN)'

gatewayd-dev-run: node-dev-prepare ## Run gatewayd in the repo-local Linux dev workspace
	$(call ensure_linux_workspace)
	$(MAKE) gateway-dashboard-assets
	exec $(GO) -C $(ROOTDIR)/gateway/gatewayd run . \
		-http-address 127.0.0.1:25080 \
		-control-edge-address 127.0.0.1:25000 \
		-control-edge-tls-ca-cert '$(NODE_DEV_DIR)/certs/ca.crt' \
		-control-edge-tls-cert '$(NODE_DEV_DIR)/certs/gatewayd.crt' \
		-control-edge-tls-key '$(NODE_DEV_DIR)/certs/gatewayd.key' \
		-tunnel-relay-target 127.0.0.1:24100 \
		-tunnel-relay-tls-ca-cert '$(NODE_DEV_DIR)/certs/ca.crt' \
		-dashboard-enabled \
		-dashboard-vendor-dir '$(ROOTDIR)/gateway/gatewayd/internal/api/http/dashboard/vendor' \
		-control-target 127.0.0.1:24000 \
		-tls-ca-cert '$(NODE_DEV_DIR)/certs/ca.crt' \
		-tls-cert '$(NODE_DEV_DIR)/certs/gatewayd.crt' \
		-tls-key '$(NODE_DEV_DIR)/certs/gatewayd.key' \
		-dev-token '$(AXERN_DEV_TOKEN)'

axern-dev: node-dev-prepare ## Run the product CLI against the standalone control plane, with ARGS='<args>'
	$(call ensure_linux_workspace)
	exec $(GO) -C $(ROOTDIR)/apps/cli run . $(ARGS)

axern-dev-build: ## Build the product CLI for standalone dev use
	mkdir -p $(ROOTDIR)/bin
	$(GO) -C $(ROOTDIR)/apps/cli build -o $(ROOTDIR)/bin/axern .

axctl-dev: node-dev-prepare ## Run axctl against the standalone axnoded socket, with ARGS='<args>'
	$(call ensure_linux_workspace)
	exec $(GO) -C $(ROOTDIR)/runtime/axnoded run ./axctl \
		--address '$(AXCTL_DEV_ADDRESS)' \
		$(ARGS)

axctl-dev-build: ## Build axctl for standalone dev use
	mkdir -p $(ROOTDIR)/bin
	$(GO) -C $(ROOTDIR)/runtime/axnoded build -o $(ROOTDIR)/bin/axctl ./axctl

dev-runtime-images-build: ## Build standalone runtime images, with DEV_RUNTIME_IMAGES='python311 ...'
	$(call ensure_linux_workspace)
	bash $(ROOTDIR)/scripts/devbox/runtime-images.sh build $(DEV_RUNTIME_IMAGES)

dev-runtime-images-load: node-dev-prepare ## Import standalone runtime images into imagemgr, with DEV_RUNTIME_IMAGES='python311 ...'
	$(call ensure_linux_workspace)
	bash $(ROOTDIR)/scripts/devbox/runtime-images.sh load $(DEV_RUNTIME_IMAGES)

axnoded-dev-run: node-dev-prepare ## Run axnoded in the repo-local Linux dev workspace
	$(call ensure_linux_workspace)
	rm -f '$(NODE_DEV_RUN_DIR)/axnoded.sock'
	@if [ "$$(id -u)" -eq 0 ]; then \
		exec $(GO) -C $(ROOTDIR)/runtime/axnoded run ./cmd/axnoded \
			-root '$(AXNODED_DEV_DIR)' \
			-config '$(AXNODED_DEV_DIR)/config.toml' \
			-socket '$(NODE_DEV_RUN_DIR)/axnoded.sock' \
			-grpc-address '127.0.0.1:23000' \
			-http-address '127.0.0.1:23001' \
			-log-level debug \
			-log-file '$(AXNODED_DEV_DIR)/logs/axnoded.log'; \
	elif command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then \
		exec env GO='$(GO)' '$(ROOTDIR)/scripts/devbox/sudo-go.sh' -C '$(ROOTDIR)/runtime/axnoded' run ./cmd/axnoded \
			-root '$(AXNODED_DEV_DIR)' \
			-config '$(AXNODED_DEV_DIR)/config.toml' \
			-socket '$(NODE_DEV_RUN_DIR)/axnoded.sock' \
			-grpc-address '127.0.0.1:23000' \
			-http-address '127.0.0.1:23001' \
			-log-level debug \
			-log-file '$(AXNODED_DEV_DIR)/logs/axnoded.log'; \
	else \
		echo "axnoded requires passwordless sudo inside the Linux workspace."; \
		exit 1; \
	fi

imagemgr-dev-run: node-dev-prepare imagefsd-build ## Run imagemgr in the repo-local Linux dev workspace
	$(call ensure_linux_workspace)
	rm -f '$(NODE_DEV_RUN_DIR)/imagemgr.sock'
	@if [ "$$(id -u)" -eq 0 ]; then \
		exec $(GO) -C $(ROOTDIR)/runtime/imagemgr run ./cmd/imagemgr \
			-debug \
			-root '$(IMAGEMGR_DEV_DIR)' \
			-imagefsd_bin '$(ROOTDIR)/target/debug/imagefsd' \
			-oss_template '$(ROOTDIR)/runtime/imagemgr/configs/oss_backend.json.example' \
			-nydus_template '$(ROOTDIR)/runtime/imagemgr/configs/nydus_registry.json.example' \
			-oss_auths_path '$(ROOTDIR)/runtime/imagemgr/oss_auths.json.example' \
			-registry_auths_path '$(ROOTDIR)/runtime/imagemgr/registry_auths.json.example' \
			-http_sock '$(NODE_DEV_RUN_DIR)/imagemgr.sock'; \
	elif command -v sudo >/dev/null 2>&1 && sudo -n true >/dev/null 2>&1; then \
		exec env GO='$(GO)' '$(ROOTDIR)/scripts/devbox/sudo-go.sh' -C '$(ROOTDIR)/runtime/imagemgr' run ./cmd/imagemgr \
			-debug \
			-root '$(IMAGEMGR_DEV_DIR)' \
			-imagefsd_bin '$(ROOTDIR)/target/debug/imagefsd' \
			-oss_template '$(ROOTDIR)/runtime/imagemgr/configs/oss_backend.json.example' \
			-nydus_template '$(ROOTDIR)/runtime/imagemgr/configs/nydus_registry.json.example' \
			-oss_auths_path '$(ROOTDIR)/runtime/imagemgr/oss_auths.json.example' \
			-registry_auths_path '$(ROOTDIR)/runtime/imagemgr/registry_auths.json.example' \
			-http_sock '$(NODE_DEV_RUN_DIR)/imagemgr.sock'; \
	else \
		echo "imagemgr requires passwordless sudo inside the Linux workspace."; \
		exit 1; \
	fi

volumed-dev-run: node-dev-prepare ## Run volumed in the repo-local Linux dev workspace
	$(call ensure_linux_workspace)
	rm -f '$(NODE_DEV_RUN_DIR)/volumed.sock'
	exec $(GO) -C $(ROOTDIR)/runtime/volumed run ./cmd/volumed \
		-root '$(VOLUMED_DEV_DIR)' \
		-socket '$(NODE_DEV_RUN_DIR)/volumed.sock' \
		-local-root '$(VOLUMED_DEV_DIR)/local'

egressd-dev-run: node-dev-prepare ## Run egressd in the repo-local Linux dev workspace
	$(call ensure_linux_workspace)
	rm -f '$(NODE_DEV_RUN_DIR)/egressd.sock'
	exec $(GO) -C $(ROOTDIR)/runtime/egressd run ./cmd/egressd \
		-root '$(EGRESSD_DEV_DIR)' \
		-socket '$(NODE_DEV_RUN_DIR)/egressd.sock'

imagefsd-dev-serve-chunk: node-dev-prepare imagefsd-build ## Run imagefsd chunk server in the repo-local Linux dev workspace
	$(call ensure_linux_workspace)
	rm -f '$(NODE_DEV_RUN_DIR)/imagefsd-chunk.sock'
	$(ROOTDIR)/target/debug/imagefsd --verbose serve-chunk \
		--chunk-db-dir '$(IMAGEFSD_DEV_DIR)/chunkdb' \
		--listen-port 9876 \
		--chunk-server-sock '$(NODE_DEV_RUN_DIR)/imagefsd-chunk.sock' \
		--log-file '$(IMAGEFSD_DEV_DIR)/imagefsd.log'

dev-stack-up: ## Start the standalone Axern dev stack inside the Linux workspace
	$(call ensure_linux_workspace)
	bash $(ROOTDIR)/scripts/devbox/stack.sh up

dev-stack-status: ## Show standalone Axern dev stack status
	$(call ensure_linux_workspace)
	bash $(ROOTDIR)/scripts/devbox/stack.sh status

dev-stack-down: ## Stop the standalone Axern dev stack
	$(call ensure_linux_workspace)
	bash $(ROOTDIR)/scripts/devbox/stack.sh down

dev-stack-restart: ## Restart one standalone dev stack service, with SERVICE=<name>
	$(call ensure_linux_workspace)
	bash $(ROOTDIR)/scripts/devbox/stack.sh restart $(SERVICE)

dev-stack-logs: ## List standalone dev stack logs, or tail SERVICE=<name>
	$(call ensure_linux_workspace)
	bash $(ROOTDIR)/scripts/devbox/stack.sh logs $(SERVICE)

dev-stack-reset: ## Stop and remove standalone dev stack database/process state
	$(call ensure_linux_workspace)
	bash $(ROOTDIR)/scripts/devbox/stack.sh reset

devbox-image-build: ## Build the root node-stack devbox image
	cd $(ROOTDIR) && $(DEVBOX) build \
		--project-dir $(ROOTDIR) \
		--platform $(DEVBOX_PLATFORM) \
		--image $(DEVBOX_IMAGE) \
		--apt-mirror-source $(DEVBOX_APT_MIRROR_SOURCE) \
		--build-proxy $(DEVBOX_BUILD_PROXY)

devbox-up: ## Start the root devbox from the repository root
	cd $(ROOTDIR) && $(DEVBOX) up \
		--project-dir $(ROOTDIR) \
		--platform $(DEVBOX_PLATFORM) \
		--image $(DEVBOX_IMAGE) \
		--container-name $(DEVBOX_CONTAINER_NAME) \
		--ssh-port $(DEVBOX_SSH_PORT) \
		--ssh-config-host $(DEVBOX_SSH_CONFIG_HOST) \
		--ssh-config-path $(DEVBOX_SSH_CONFIG_PATH) \
		--apt-mirror-source $(DEVBOX_APT_MIRROR_SOURCE) \
		--build-proxy $(DEVBOX_BUILD_PROXY)

devbox-status: ## Show root devbox status
	cd $(ROOTDIR) && $(DEVBOX) status \
		--container-name $(DEVBOX_CONTAINER_NAME)

devbox-down: ## Stop the root devbox
	cd $(ROOTDIR) && $(DEVBOX) down \
		--container-name $(DEVBOX_CONTAINER_NAME)

devbox-shell: ## Open a shell in the root devbox
	cd $(ROOTDIR) && $(DEVBOX) shell \
		--project-dir $(ROOTDIR) \
		--container-name $(DEVBOX_CONTAINER_NAME)

devbox-stack-up: ## Start the standalone Axern dev stack inside the running devbox
	cd $(ROOTDIR) && $(DEVBOX) exec \
		--project-dir $(ROOTDIR) \
		--container-name $(DEVBOX_CONTAINER_NAME) \
		-- make dev-stack-up

devbox-stack-status: ## Show standalone Axern dev stack status inside the running devbox
	cd $(ROOTDIR) && $(DEVBOX) exec \
		--project-dir $(ROOTDIR) \
		--container-name $(DEVBOX_CONTAINER_NAME) \
		-- make dev-stack-status

devbox-stack-down: ## Stop the standalone Axern dev stack inside the running devbox
	cd $(ROOTDIR) && $(DEVBOX) exec \
		--project-dir $(ROOTDIR) \
		--container-name $(DEVBOX_CONTAINER_NAME) \
		-- make dev-stack-down

devbox-stack-restart: ## Restart one standalone dev stack service inside the running devbox, with SERVICE=<name>
	cd $(ROOTDIR) && $(DEVBOX) exec \
		--project-dir $(ROOTDIR) \
		--container-name $(DEVBOX_CONTAINER_NAME) \
		-- make dev-stack-restart SERVICE=$(SERVICE)

devbox-stack-logs: ## List standalone dev stack logs inside the running devbox, or tail SERVICE=<name>
	cd $(ROOTDIR) && $(DEVBOX) exec \
		--project-dir $(ROOTDIR) \
		--container-name $(DEVBOX_CONTAINER_NAME) \
		-- make dev-stack-logs SERVICE=$(SERVICE)

devbox-stack-reset: ## Reset standalone Axern dev stack state inside the running devbox
	cd $(ROOTDIR) && $(DEVBOX) exec \
		--project-dir $(ROOTDIR) \
		--container-name $(DEVBOX_CONTAINER_NAME) \
		-- make dev-stack-reset

devbox-runtime-images-load: ## Import standalone runtime images inside the running devbox, with DEV_RUNTIME_IMAGES='python311 ...'
	cd $(ROOTDIR) && $(DEVBOX) exec \
		--project-dir $(ROOTDIR) \
		--container-name $(DEVBOX_CONTAINER_NAME) \
		-- make dev-runtime-images-load DEV_RUNTIME_IMAGES='$(DEV_RUNTIME_IMAGES)'

devbox-axern: ## Run the product CLI inside the running devbox, with ARGS='<args>'
	cd $(ROOTDIR) && $(DEVBOX) exec \
		--project-dir $(ROOTDIR) \
		--container-name $(DEVBOX_CONTAINER_NAME) \
		-- make axern-dev ARGS='$(ARGS)'

devbox-axctl: ## Run axctl inside the running devbox, with ARGS='<args>'
	cd $(ROOTDIR) && $(DEVBOX) exec \
		--project-dir $(ROOTDIR) \
		--container-name $(DEVBOX_CONTAINER_NAME) \
		-- make axctl-dev ARGS='$(ARGS)'
