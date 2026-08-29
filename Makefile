.DEFAULT_GOAL := help

ROOTDIR := $(abspath $(dir $(lastword $(MAKEFILE_LIST))))
MK_DIR := $(ROOTDIR)/mk

PNPM ?= pnpm
GOROOT ?= $(shell go env GOROOT)
GO ?= $(GOROOT)/bin/go
UV ?= uv
CARGO ?= cargo
DEVBOX ?= $(ROOTDIR)/scripts/devbox/devbox.sh
DEVBOX_PLATFORM ?= linux/arm64
DEVBOX_IMAGE ?= axern-devbox:latest-arm64
DEVBOX_CONTAINER_NAME ?= axern-devbox
DEVBOX_SSH_PORT ?= 2222
DEVBOX_SSH_CONFIG_HOST ?= $(DEVBOX_CONTAINER_NAME)
DEVBOX_SSH_CONFIG_PATH ?= $(HOME)/.ssh/config
DEVBOX_APT_MIRROR_SOURCE ?= archive
DEVBOX_BUILD_PROXY ?= none

include mk/common.mk
include mk/devbox.mk
include mk/dev-env.mk
include mk/axrun.mk
include mk/root.mk
include mk/deploy.mk
include mk/subsystems/axnoded.mk
include mk/subsystems/controld.mk
include mk/subsystems/egressd.mk
include mk/subsystems/storaged.mk
include mk/subsystems/volumed.mk

.PHONY: help list-targets

help: ## Show available targets
	@echo "Usage: make <target>"
	@echo ""
	@echo "Targets:"
	@grep -h -E '^[a-zA-Z0-9_-]+:.*## .*$$' $(MAKEFILE_LIST) | sort | \
		awk 'BEGIN {FS = ":.*## "}; {printf "  %-32s %s\n", $$1, $$2}'

list-targets: ## List available make targets for shell completion
	@awk -F: '/^[a-zA-Z0-9][a-zA-Z0-9_.-]*:/{print $$1}' $(MAKEFILE_LIST) | sort -u
