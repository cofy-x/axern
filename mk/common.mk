SUBSYSTEM_FORWARD_VARS := \
	GOROOT \
	GO \
	GOOS \
	GOARCH \
	EXTRA_TESTFLAGS \
	RUNTIME_UNDER_TEST \
	DEVBOX \
	DEVBOX_PLATFORM \
	DEVBOX_IMAGE \
	DEVBOX_CONTAINER_NAME \
	DEVBOX_SSH_PORT \
	DEVBOX_SSH_CONFIG_HOST \
	DEVBOX_SSH_CONFIG_PATH \
	DEVBOX_APT_MIRROR_SOURCE \
	DEVBOX_BUILD_PROXY

define forward_make_assignments
$(strip $(foreach var,$(SUBSYSTEM_FORWARD_VARS),$(if $(strip $($(var))),$(var)='$(strip $($(var)))',)))
endef

define run_subsystem_make
$(call forward_make_assignments) $(MAKE) -C $(1) $(2)
endef
