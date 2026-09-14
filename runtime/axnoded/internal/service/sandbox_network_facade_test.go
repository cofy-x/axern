package service

import (
	"testing"

	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/stretchr/testify/require"
)

func TestConfigureNetworkingDefersContainerManagerLookup(t *testing.T) {
	base := newTestService(t,
		&runtimeSpyHandler{name: "runsc"},
	)
	containerID := "axctl-networking-deferred-lookup"
	_, err := base.containerManager.Occupy(
		resourcemanager.AllocateOption{ContainerID: containerID},
		resourcemanager.InterfaceResourceName,
	)
	require.NoError(t, err)

	early := &sandboxService{
		config: base.config,
		store:  base.store,
	}
	early.configureNetworking()
	early.containerManager = base.containerManager

	ip, err := early.sandboxNetworking().ContainerIP(containerID)
	require.NoError(t, err)
	require.Equal(t, "10.0.0.20", ip)
}
