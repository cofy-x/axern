package networking

import (
	"context"
	"fmt"
	"net"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	networkmanager "github.com/cofy-x/axern/runtime/axnoded/internal/network"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkForSandboxReturnsResource(t *testing.T) {
	c := NewCoordinator(Options{CollectResourceByID: func(id string) (container.OccupiedResource, error) {
		return newNetworkResource(id, "10.0.0.9", "/var/run/netns/ctr"), nil
	}})

	network, err := c.NetworkForSandbox("ctr")
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.9", network.IP)
	assert.Equal(t, "/var/run/netns/ctr", network.NetNSPath)
}

func TestCleanupActivationNetwork(t *testing.T) {
	fake := &fakeNetworkManager{}
	c := newTestCoordinator(fake)

	err := c.CleanupActivationNetwork(newNetworkResource("ctr", "10.0.0.10", "/var/run/netns/ctr"))
	require.NoError(t, err)
	assert.Equal(t, []string{"10.0.0.10"}, fake.activationCleanups)
}

func TestProbePortUsesAllocationNetwork(t *testing.T) {
	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	c := NewCoordinator(Options{
		CollectResourceByID: func(id string) (container.OccupiedResource, error) {
			return newNetworkResource(id, "10.0.0.11", "/var/run/netns/ctr"), nil
		},
		DialContext: func(_ context.Context, network, address string) (net.Conn, error) {
			if network != "tcp" || address != "10.0.0.11:8080" {
				return nil, fmt.Errorf("dial %s %s", network, address)
			}
			return clientConn, nil
		},
	})
	done := make(chan struct{})
	go func() {
		_, _ = serverConn.Read(make([]byte, 1))
		_ = serverConn.Close()
		close(done)
	}()

	require.NoError(t, c.ProbePort(context.Background(), "ctr", 8080))
	<-done
}

type fakeNetworkManager struct {
	activationCleanups []string
}

func (f *fakeNetworkManager) SetupSNATRules(string) error                         { return nil }
func (f *fakeNetworkManager) CleanupSNATRules(string) error                       { return nil }
func (f *fakeNetworkManager) SetupNetworkRulesForActivating(net.IP, string) error { return nil }
func (f *fakeNetworkManager) CleanupNetworkRulesForActivating(ip net.IP) error {
	f.activationCleanups = append(f.activationCleanups, ip.String())
	return nil
}

func newTestCoordinator(fake networkmanager.NetworkManager) *Coordinator {
	return NewCoordinator(Options{
		NatBackend: "test",
		CollectResourceByID: func(id string) (container.OccupiedResource, error) {
			return newNetworkResource(id, "10.0.0.2", "/var/run/netns/"+id), nil
		},
		NetworkManager: func(string) (networkmanager.NetworkManager, bool) { return fake, true },
	})
}

func newNetworkResource(id, ip, netns string) container.OccupiedResource {
	netResource := &resourcemanager.NetResource{Ip: net.ParseIP(ip), NetNSPath: netns}
	return container.OccupiedResource{
		ID: id,
		Resources: map[resourcemanager.ResourceName]string{
			resourcemanager.InterfaceResourceName: netResource.ToString(),
		},
	}
}
