package networking

import (
	"fmt"
	"net"

	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type SandboxNetwork struct {
	IP           string
	NetNSPath    string
	RuntimeClass string
}

func (c *Coordinator) NetworkForSandbox(containerID string) (*SandboxNetwork, error) {
	if containerID == "" {
		return nil, errord.ErrInvalidArgument
	}
	resource, err := c.resourceForContainer(containerID)
	if err != nil {
		return nil, err
	}
	netDevice, err := netResourceFromOccupied(resource)
	if err != nil {
		return nil, err
	}
	if netDevice.Ip == nil || netDevice.Ip.String() == "" {
		return nil, fmt.Errorf("container has no ip: %w", errord.ErrFailedPrecondition)
	}
	if netDevice.NetNSPath == "" {
		return nil, fmt.Errorf("container has no netns path: %w", errord.ErrFailedPrecondition)
	}
	runtimeClass := ""
	if c.runtimeClass != nil {
		runtimeClass, err = c.runtimeClass(containerID)
		if err != nil {
			return nil, err
		}
	}
	return &SandboxNetwork{IP: netDevice.Ip.String(), NetNSPath: netDevice.NetNSPath, RuntimeClass: runtimeClass}, nil
}

func (c *Coordinator) ContainerIP(containerID string) (string, error) {
	resource, err := c.resourceForContainer(containerID)
	if err != nil {
		return "", err
	}
	netDevice, err := netResourceFromOccupied(resource)
	if err != nil {
		return "", err
	}
	if netDevice.Ip == nil || netDevice.Ip.String() == "" {
		return "", fmt.Errorf("container has no ip: %w", errord.ErrFailedPrecondition)
	}
	return netDevice.Ip.String(), nil
}

func (c *Coordinator) CleanupActivationNetwork(resource container.OccupiedResource) error {
	device, ok := resource.ToLabels()[resourcemanager.ResourceAnnotationKeyPrefix+string(resourcemanager.InterfaceResourceName)]
	if !ok {
		return nil
	}
	netDevice := &resourcemanager.NetResource{}
	if err := netDevice.FromString(device); err != nil {
		return fmt.Errorf("parse net device(%s) failed, err: %v", device, err)
	}
	if err := c.CleanupActivationNetworkByIP(netDevice.Ip); err != nil {
		return fmt.Errorf("cleanup network rules for activating %s failed, err: %v", netDevice.Ip, err)
	}
	return nil
}

func (c *Coordinator) CleanupActivationNetworkByIP(ip net.IP) error {
	m, ok := c.networkManager(c.natBackend)
	if !ok {
		return fmt.Errorf("no corresponding network manager for networkType: %s", c.natBackend)
	}
	return m.CleanupNetworkRulesForActivating(ip)
}

func (c *Coordinator) resourceForContainer(containerID string) (container.OccupiedResource, error) {
	if c.collectResourceByID == nil {
		return container.OccupiedResource{}, fmt.Errorf("container resource lookup is not configured")
	}
	return c.collectResourceByID(containerID)
}

func netResourceFromOccupied(resource container.OccupiedResource) (*resourcemanager.NetResource, error) {
	device, ok := resource.Resources[resourcemanager.InterfaceResourceName]
	if !ok {
		return nil, fmt.Errorf("container has no network interface: %w", errord.ErrFailedPrecondition)
	}
	netDevice := &resourcemanager.NetResource{}
	if err := netDevice.FromString(device); err != nil {
		return nil, err
	}
	return netDevice, nil
}
