.PHONY: \
	helm-lint \
	helm-contract-check \
	helm-template \
	helm-render \
	helm-dry-run \
	helm-install \
	helm-status \
	helm-pods \
	helm-health \
	helm-gateway-ssh-secret \
	helm-kube-context \
	helm-registry-secret \
	helm-port-forward \
	helm-observability-port-forward \
	helm-axern-context \
	helm-uninstall

KUBECTL ?= kubectl
HELM ?= helm
AXERN_HELM_UPGRADE_ARGS ?= --server-side=true --force-conflicts

AXERN_HELM_CHART ?= $(ROOTDIR)/deploy/helm/axern
AXERN_HELM_RELEASE ?= axern
AXERN_HELM_NAMESPACE ?= axern-system
AXERN_HELM_VALUES ?=
AXERN_HELM_RENDER_DIR ?= $(ROOTDIR)/deploy/local/state/helm/$(AXERN_HELM_RELEASE)
AXERN_HELM_RENDER_FILE ?= $(AXERN_HELM_RENDER_DIR)/rendered.yaml
AXERN_KUBECONFIG ?=
AXERN_KUBE_CONTEXT ?=

AXERN_REGISTRY_PULL_SECRET ?= registry-pull
AXERN_REGISTRY_SERVER ?=
AXERN_REGISTRY_USERNAME ?=
AXERN_REGISTRY_PASSWORD ?=

AXERN_CLI_CONFIG ?= $(HOME)/.config/axern/config.json
AXERN_CLI_CONTEXT ?= $(AXERN_HELM_RELEASE)
AXERN_CLI_STATE_DIR ?= $(AXERN_HELM_RENDER_DIR)/axern-cli
AXERN_CLI_CERT_DIR ?= $(AXERN_CLI_STATE_DIR)/certs
AXERN_CLI_SSH_DIR ?= $(AXERN_CLI_STATE_DIR)/ssh
AXERN_CLI_PKI_SECRET ?= controld-pki
AXERN_CLI_ENDPOINT ?= 127.0.0.1:$(AXERN_GATEWAYD_CONTROL_PORT)
AXERN_CLI_SERVICE_URL ?= http://127.0.0.1:$(AXERN_GATEWAYD_HTTP_PORT)
AXERN_CLI_SSH_ENDPOINT ?= 127.0.0.1:$(AXERN_GATEWAYD_SSH_PORT)
AXERN_CLI_SSH_IDENTITY_FILE ?= $(AXERN_CLI_SSH_DIR)/gateway_client_ed25519
AXERN_CLI_TLS_SERVER_NAME ?=
AXERN_CLI_PROXY_MODE ?=
AXERN_CONTROLD_HEALTH_PORT ?= 24131
AXERN_GATEWAYD_CONTROL_PORT ?= 25100
AXERN_GATEWAYD_HTTP_PORT ?= 25101
AXERN_GATEWAYD_HEALTH_PORT ?= $(AXERN_GATEWAYD_HTTP_PORT)
AXERN_GATEWAYD_SSH_PORT ?= 25122
AXERN_GRAFANA_PORT ?= 13002
AXERN_GATEWAYD_SSH_SECRET ?= gatewayd-ssh
AXERN_GATEWAYD_SSH_HOST_KEY ?= $(AXERN_CLI_SSH_DIR)/gateway_host_ed25519
AXERN_GATEWAYD_SSH_AUTHORIZED_KEYS ?= $(AXERN_CLI_SSH_DIR)/authorized_keys

define require_kube_context
	@if [ -n "$(strip $(AXERN_KUBECONFIG))" ] && [ ! -f "$(strip $(AXERN_KUBECONFIG))" ]; then \
		echo "AXERN_KUBECONFIG does not exist: $(AXERN_KUBECONFIG)"; \
		exit 1; \
	elif [ -n "$(strip $(AXERN_KUBE_CONTEXT))" ]; then \
		current_context="$$($(KUBECTL) $(call kubectl_kubeconfig_arg) config current-context 2>/dev/null || true)"; \
		if [ "$${current_context}" != "$(AXERN_KUBE_CONTEXT)" ]; then \
			echo "kubectl current-context is '$${current_context}', expected '$(AXERN_KUBE_CONTEXT)'."; \
			echo "Run 'make helm-kube-context' or pass the right AXERN_KUBECONFIG/AXERN_KUBE_CONTEXT."; \
			exit 1; \
		fi; \
	elif [ -n "$(strip $(AXERN_KUBECONFIG))" ]; then \
		current_context="$$($(KUBECTL) $(call kubectl_kubeconfig_arg) config current-context 2>/dev/null || true)"; \
		if [ -z "$${current_context}" ]; then \
			echo "AXERN_KUBECONFIG has no current context: $(AXERN_KUBECONFIG)"; \
			exit 1; \
		fi; \
	else \
		echo "AXERN_KUBECONFIG is required for this target."; \
		exit 1; \
	fi
endef

define require_helm_values
	@if [ -z "$(strip $(AXERN_HELM_VALUES))" ]; then \
		echo "AXERN_HELM_VALUES is required for this target."; \
		exit 1; \
	fi
endef

define require_registry_server
	@if [ -z "$(strip $(AXERN_REGISTRY_SERVER))" ]; then \
		echo "AXERN_REGISTRY_SERVER is required for this target."; \
		exit 1; \
	fi
endef

define kubectl_context_arg
$(if $(strip $(AXERN_KUBE_CONTEXT)),--context '$(strip $(AXERN_KUBE_CONTEXT))')
endef

define kubectl_kubeconfig_arg
$(if $(strip $(AXERN_KUBECONFIG)),--kubeconfig '$(strip $(AXERN_KUBECONFIG))')
endef

define kubectl_args
$(call kubectl_kubeconfig_arg) $(call kubectl_context_arg)
endef

define helm_context_arg
$(if $(strip $(AXERN_KUBE_CONTEXT)),--kube-context '$(strip $(AXERN_KUBE_CONTEXT))')
endef

define helm_kubeconfig_arg
$(if $(strip $(AXERN_KUBECONFIG)),--kubeconfig '$(strip $(AXERN_KUBECONFIG))')
endef

define helm_args
$(call helm_kubeconfig_arg) $(call helm_context_arg)
endef

define helm_common_args
--namespace '$(AXERN_HELM_NAMESPACE)' \
--values '$(AXERN_HELM_VALUES)'
endef

helm-lint: helm-contract-check ## Lint the Axern Helm chart
	$(HELM) lint '$(AXERN_HELM_CHART)'

helm-contract-check: ## Verify Helm values preserve runtime argument contracts
	@value="$$($(HELM) template axern-contract-check '$(AXERN_HELM_CHART)' | \
		awk '/- -artifact-max-bytes/{getline; gsub(/[- "'"'"']/, ""); print; exit}')"; \
		test "$$value" = "8589934592" || { \
			echo "artifact max bytes rendered as invalid integer: $$value" >&2; \
			exit 1; \
		}

helm-template: ## Render the Axern Helm chart for the configured namespace and values
	$(call require_helm_values)
	$(HELM) template '$(AXERN_HELM_RELEASE)' '$(AXERN_HELM_CHART)' \
		$(call helm_common_args)

helm-render: ## Render the Axern Helm chart to deploy/local/state for review
	$(call require_helm_values)
	mkdir -p '$(AXERN_HELM_RENDER_DIR)'
	$(HELM) template '$(AXERN_HELM_RELEASE)' '$(AXERN_HELM_CHART)' \
		$(call helm_common_args) > '$(AXERN_HELM_RENDER_FILE)'
	@echo "Rendered Helm manifest: $(AXERN_HELM_RENDER_FILE)"

helm-dry-run: ## Render the Axern Helm chart and validate it with kubectl dry-run
	$(call require_kube_context)
	$(call require_helm_values)
	$(HELM) template '$(AXERN_HELM_RELEASE)' '$(AXERN_HELM_CHART)' \
		$(call helm_common_args) | \
		$(KUBECTL) $(call kubectl_args) apply --dry-run=client -f -

helm-kube-context: ## Switch kubectl to the configured Kubernetes context
	@if [ -z "$(strip $(AXERN_KUBE_CONTEXT))" ]; then \
		echo "AXERN_KUBE_CONTEXT is required."; \
		exit 1; \
	fi
	$(KUBECTL) $(call kubectl_kubeconfig_arg) config use-context '$(AXERN_KUBE_CONTEXT)'

helm-registry-secret: ## Ensure a container registry pull secret exists in the Helm namespace
	$(call require_kube_context)
	$(call require_registry_server)
	@if $(KUBECTL) $(call kubectl_args) get namespace '$(AXERN_HELM_NAMESPACE)' >/dev/null 2>&1; then \
		echo "Namespace already exists: $(AXERN_HELM_NAMESPACE)"; \
	else \
		$(KUBECTL) $(call kubectl_args) create namespace '$(AXERN_HELM_NAMESPACE)'; \
	fi
	@if $(KUBECTL) $(call kubectl_args) -n '$(AXERN_HELM_NAMESPACE)' get secret '$(AXERN_REGISTRY_PULL_SECRET)' >/dev/null 2>&1; then \
		echo "Registry pull secret already exists: $(AXERN_HELM_NAMESPACE)/$(AXERN_REGISTRY_PULL_SECRET)"; \
	elif [ -n "$(strip $(AXERN_REGISTRY_USERNAME))" ] && [ -n "$(strip $(AXERN_REGISTRY_PASSWORD))" ]; then \
		$(KUBECTL) $(call kubectl_args) -n '$(AXERN_HELM_NAMESPACE)' create secret docker-registry '$(AXERN_REGISTRY_PULL_SECRET)' \
			--docker-server='$(AXERN_REGISTRY_SERVER)' \
			--docker-username='$(AXERN_REGISTRY_USERNAME)' \
			--docker-password='$(AXERN_REGISTRY_PASSWORD)'; \
	else \
		echo "Missing registry pull secret: $(AXERN_HELM_NAMESPACE)/$(AXERN_REGISTRY_PULL_SECRET)"; \
		echo "Provide AXERN_REGISTRY_USERNAME and AXERN_REGISTRY_PASSWORD, or create the secret manually."; \
		exit 1; \
	fi

helm-install: helm-lint helm-dry-run ## Install or upgrade Axern with Helm
	$(call require_kube_context)
	$(call require_helm_values)
	$(HELM) $(call helm_args) upgrade --install '$(AXERN_HELM_RELEASE)' '$(AXERN_HELM_CHART)' \
		$(call helm_common_args) \
		--create-namespace \
		$(AXERN_HELM_UPGRADE_ARGS) \
		--wait --timeout 10m

helm-status: ## Show the configured Helm release status
	$(call require_kube_context)
	$(HELM) $(call helm_args) status '$(AXERN_HELM_RELEASE)' --namespace '$(AXERN_HELM_NAMESPACE)'

helm-pods: ## Show pods and services in the configured Helm namespace
	$(call require_kube_context)
	$(KUBECTL) $(call kubectl_args) get pods,svc -n '$(AXERN_HELM_NAMESPACE)' -o wide

helm-health: ## Check service health and the current node-report contract
	$(call require_kube_context)
	@bash -eu -o pipefail -c '\
		controld_log="$${TMPDIR:-/tmp}/axern-controld-port-forward.log"; \
		gatewayd_log="$${TMPDIR:-/tmp}/axern-gatewayd-port-forward.log"; \
		nodes_out="$${TMPDIR:-/tmp}/axern-controld-nodes.out"; \
		rm -f "$${controld_log}" "$${gatewayd_log}" "$${nodes_out}"; \
		$(KUBECTL) $(call kubectl_args) -n "$(AXERN_HELM_NAMESPACE)" port-forward svc/controld "$(AXERN_CONTROLD_HEALTH_PORT):24001" >"$${controld_log}" 2>&1 & controld_pf=$$!; \
		$(KUBECTL) $(call kubectl_args) -n "$(AXERN_HELM_NAMESPACE)" port-forward svc/gatewayd "$(AXERN_GATEWAYD_HEALTH_PORT):25080" >"$${gatewayd_log}" 2>&1 & gatewayd_pf=$$!; \
		cleanup() { kill "$${controld_pf}" "$${gatewayd_pf}" >/dev/null 2>&1 || true; }; \
		trap cleanup EXIT; \
		for _ in $$(seq 1 30); do \
			if curl -fsS "http://127.0.0.1:$(AXERN_CONTROLD_HEALTH_PORT)/healthz" >/tmp/axern-controld-health.out 2>/tmp/axern-controld-health.err; then break; fi; \
			sleep 1; \
		done; \
		for _ in $$(seq 1 30); do \
			if curl -fsS "http://127.0.0.1:$(AXERN_GATEWAYD_HEALTH_PORT)/healthz" >/tmp/axern-gatewayd-health.out 2>/tmp/axern-gatewayd-health.err; then break; fi; \
			sleep 1; \
		done; \
		for _ in $$(seq 1 60); do \
			if curl -fsS "http://127.0.0.1:$(AXERN_CONTROLD_HEALTH_PORT)/nodesz" >"$${nodes_out}" 2>/dev/null && \
				python3 -c "import json,sys; nodes=json.load(open(sys.argv[1], encoding=\"utf-8\")).get(\"nodes\", []); raise SystemExit(0 if nodes and all(node.get(\"fresh\") and node.get(\"summary_fresh\") and ((((node.get(\"summary\") or {}).get(\"pools\") or {}).get(\"runtime_slots\")) is not None) for node in nodes) else 1)" "$${nodes_out}"; then \
				break; \
			fi; \
			sleep 1; \
		done; \
		python3 -c "import json,sys; nodes=json.load(open(sys.argv[1], encoding=\"utf-8\")).get(\"nodes\", []); invalid=[node.get(\"node_id\", \"<unknown>\") for node in nodes if not node.get(\"fresh\") or not node.get(\"summary_fresh\") or ((((node.get(\"summary\") or {}).get(\"pools\") or {}).get(\"runtime_slots\")) is None)]; print(f\"node report contract: nodes={len(nodes)} invalid={len(invalid)}\"); raise SystemExit(0 if nodes and not invalid else 1)" "$${nodes_out}"; \
		printf "controld health: "; cat /tmp/axern-controld-health.out; printf "\n"; \
		printf "gatewayd health: "; cat /tmp/axern-gatewayd-health.out; printf "\n"; \
	'

helm-gateway-ssh-secret: ## Ensure the gatewayd SSH host key and authorized client key secret exists
	$(call require_kube_context)
	mkdir -p '$(AXERN_CLI_SSH_DIR)'
	@if [ ! -s '$(AXERN_GATEWAYD_SSH_HOST_KEY)' ]; then \
		ssh-keygen -q -t ed25519 -N "" -f '$(AXERN_GATEWAYD_SSH_HOST_KEY)' -C '$(AXERN_HELM_RELEASE)-gatewayd-host' >/dev/null; \
	fi
	@if [ ! -s '$(AXERN_CLI_SSH_IDENTITY_FILE)' ]; then \
		ssh-keygen -q -t ed25519 -N "" -f '$(AXERN_CLI_SSH_IDENTITY_FILE)' -C '$(AXERN_HELM_RELEASE)-gatewayd-client' >/dev/null; \
	fi
	cat '$(AXERN_CLI_SSH_IDENTITY_FILE).pub' > '$(AXERN_GATEWAYD_SSH_AUTHORIZED_KEYS)'
	chmod 700 '$(AXERN_CLI_SSH_DIR)'
	chmod 600 '$(AXERN_GATEWAYD_SSH_HOST_KEY)' '$(AXERN_CLI_SSH_IDENTITY_FILE)' '$(AXERN_GATEWAYD_SSH_AUTHORIZED_KEYS)'
	$(KUBECTL) $(call kubectl_args) -n '$(AXERN_HELM_NAMESPACE)' create secret generic '$(AXERN_GATEWAYD_SSH_SECRET)' \
		--from-file=gateway_host_ed25519='$(AXERN_GATEWAYD_SSH_HOST_KEY)' \
		--from-file=authorized_keys='$(AXERN_GATEWAYD_SSH_AUTHORIZED_KEYS)' \
		--dry-run=client -o yaml | \
		$(KUBECTL) $(call kubectl_args) apply -f -

helm-port-forward: ## Keep local port-forwards open for CLI and gateway access
	$(call require_kube_context)
	@echo "Forwarding controld health 127.0.0.1:$(AXERN_CONTROLD_HEALTH_PORT) -> svc/controld:24001"
	@echo "Forwarding gatewayd control 127.0.0.1:$(AXERN_GATEWAYD_CONTROL_PORT) -> svc/gatewayd:25000"
	@echo "Forwarding gatewayd HTTP 127.0.0.1:$(AXERN_GATEWAYD_HTTP_PORT) -> svc/gatewayd:25080"
	@echo "Forwarding gatewayd SSH 127.0.0.1:$(AXERN_GATEWAYD_SSH_PORT) -> svc/gatewayd:25022"
	@echo "Keep this command running while using the local axern CLI context."
	$(KUBECTL) $(call kubectl_args) -n '$(AXERN_HELM_NAMESPACE)' port-forward \
		svc/controld '$(AXERN_CONTROLD_HEALTH_PORT):24001' & \
	controld_pf=$$!; \
	$(KUBECTL) $(call kubectl_args) -n '$(AXERN_HELM_NAMESPACE)' port-forward \
		svc/gatewayd '$(AXERN_GATEWAYD_CONTROL_PORT):25000' '$(AXERN_GATEWAYD_HTTP_PORT):25080' '$(AXERN_GATEWAYD_SSH_PORT):25022' & \
	gatewayd_pf=$$!; \
	cleanup() { kill "$${controld_pf}" "$${gatewayd_pf}" >/dev/null 2>&1 || true; }; \
	trap cleanup INT TERM EXIT; \
	while kill -0 "$${controld_pf}" >/dev/null 2>&1 && kill -0 "$${gatewayd_pf}" >/dev/null 2>&1; do \
		sleep 1; \
	done; \
	cleanup; \
	wait "$${controld_pf}" "$${gatewayd_pf}" >/dev/null 2>&1 || true

helm-observability-port-forward: ## Keep the local Grafana port-forward open
	$(call require_kube_context)
	@echo "Forwarding Grafana http://127.0.0.1:$(AXERN_GRAFANA_PORT) -> svc/grafana:3000"
	$(KUBECTL) $(call kubectl_args) -n '$(AXERN_HELM_NAMESPACE)' port-forward svc/grafana '$(AXERN_GRAFANA_PORT):3000'

helm-axern-context: ## Install or update a local axern CLI context from the Helm PKI secret
	$(call require_kube_context)
	mkdir -p '$(AXERN_CLI_CERT_DIR)'
	$(KUBECTL) $(call kubectl_args) -n '$(AXERN_HELM_NAMESPACE)' get secret '$(AXERN_CLI_PKI_SECRET)' -o json | \
		python3 '$(ROOTDIR)/scripts/deploy/export-k8s-secret-files.py' '$(AXERN_CLI_CERT_DIR)' ca.crt client.crt client.key
	python3 '$(ROOTDIR)/scripts/deploy/install-axern-context.py' \
		--config '$(AXERN_CLI_CONFIG)' \
		--context '$(AXERN_CLI_CONTEXT)' \
		--endpoint '$(AXERN_CLI_ENDPOINT)' \
		--tls-ca-cert '$(AXERN_CLI_CERT_DIR)/ca.crt' \
		--tls-cert '$(AXERN_CLI_CERT_DIR)/client.crt' \
		--tls-key '$(AXERN_CLI_CERT_DIR)/client.key' \
		--tls-server-name '$(AXERN_CLI_TLS_SERVER_NAME)' \
		--proxy-mode '$(AXERN_CLI_PROXY_MODE)' \
		--service-url '$(AXERN_CLI_SERVICE_URL)' \
		--ssh-endpoint '$(AXERN_CLI_SSH_ENDPOINT)' \
		--ssh-identity-file '$(AXERN_CLI_SSH_IDENTITY_FILE)' \
		--current
	@echo "Axern CLI context '$(AXERN_CLI_CONTEXT)' is ready."

helm-uninstall: ## Uninstall the configured Helm release
	$(call require_kube_context)
	$(HELM) $(call helm_args) uninstall '$(AXERN_HELM_RELEASE)' --namespace '$(AXERN_HELM_NAMESPACE)'
