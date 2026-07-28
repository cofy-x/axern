package service

import (
	"net"
	"testing"

	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/stretchr/testify/require"
)

func TestConfigureNetworkingDefersContainerManagerLookup(t *testing.T) {
	base := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": &runtimeSpyHandler{name: "runsc"},
	})
	containerID := "axctl-networking-deferred-lookup"
	netResource := &resourcemanager.NetResource{
		Ip:        net.ParseIP("10.0.0.20"),
		NetNSPath: "/var/run/netns/axctl-networking-deferred-lookup",
	}
	writeContainerSpecFile(t, base.config.RootDir, containerID, map[string]string{
		resourcemanager.ResourceAnnotationKeyPrefix + string(resourcemanager.InterfaceResourceName): netResource.ToString(),
	})

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
