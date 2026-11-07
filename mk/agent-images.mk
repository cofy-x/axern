.PHONY: agent-images agent-images-check agent-images-smoke

AGENT_IMAGE_PYTHON ?= python3
AGENT_IMAGE_TARGETS ?= base,toolchain,claude-code,codex
AGENT_IMAGE_TAG_PREFIX ?= axern-agent
AGENT_IMAGE_TAG_SUFFIX ?= local
AGENT_IMAGE_CACHE_DIR ?=
AGENT_IMAGE_DOCKER_CONFIG ?=
AGENT_IMAGE_OUTPUT_DIR ?=

export AGENT_IMAGE_PYTHON AGENT_IMAGE_TARGETS AGENT_IMAGE_TAG_PREFIX AGENT_IMAGE_TAG_SUFFIX
export AGENT_IMAGE_CACHE_DIR AGENT_IMAGE_DOCKER_CONFIG AGENT_IMAGE_OUTPUT_DIR

agent-images: ## Build local layered Agent workload images without publishing
	@set -- "$(ROOTDIR)/images/agents/build.py" \
		--targets "$${AGENT_IMAGE_TARGETS}" \
		--tag-prefix "$${AGENT_IMAGE_TAG_PREFIX}" --tag-suffix "$${AGENT_IMAGE_TAG_SUFFIX}"; \
	if [ -n "$${AGENT_IMAGE_CACHE_DIR}" ]; then set -- "$$@" --cache-dir "$${AGENT_IMAGE_CACHE_DIR}"; fi; \
	if [ -n "$${AGENT_IMAGE_DOCKER_CONFIG}" ]; then set -- "$$@" --docker-config "$${AGENT_IMAGE_DOCKER_CONFIG}"; fi; \
	if [ -n "$${AGENT_IMAGE_OUTPUT_DIR}" ]; then set -- "$$@" --output-dir "$${AGENT_IMAGE_OUTPUT_DIR}"; fi; \
	"$${AGENT_IMAGE_PYTHON}" "$$@"

agent-images-check: ## Check Agent image inputs and security contracts without Docker or models
	@"$${AGENT_IMAGE_PYTHON}" "$(ROOTDIR)/images/agents/check.py"

agent-images-smoke: ## Run explicit model-free Docker checks of built Agent images
	@set -- "$(ROOTDIR)/images/agents/smoke.py" \
		--targets "$${AGENT_IMAGE_TARGETS}" \
		--tag-prefix "$${AGENT_IMAGE_TAG_PREFIX}" --tag-suffix "$${AGENT_IMAGE_TAG_SUFFIX}"; \
	if [ -n "$${AGENT_IMAGE_DOCKER_CONFIG}" ]; then set -- "$$@" --docker-config "$${AGENT_IMAGE_DOCKER_CONFIG}"; fi; \
	if [ -n "$${AGENT_IMAGE_OUTPUT_DIR}" ]; then set -- "$$@" --output-dir "$${AGENT_IMAGE_OUTPUT_DIR}"; fi; \
	"$${AGENT_IMAGE_PYTHON}" "$$@"

# Full source validation and the existing required release-contract CI job run
# these host-safe tests. Docker builds and model calls remain explicit actions.
test release-check: agent-images-check
