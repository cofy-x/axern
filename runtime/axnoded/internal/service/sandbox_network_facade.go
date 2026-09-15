package service

import (
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	servicenetworking "github.com/cofy-x/axern/runtime/axnoded/internal/service/networking"
)

func (h *sandboxService) configureNetworking() {
	h.networking = servicenetworking.NewCoordinator(servicenetworking.Options{
		NatBackend: h.config.NatBackend,
		CollectResourceByID: func(id string) (container.OccupiedResource, error) {
			return h.containerManager.CollectResourceByID(id)
		},
		ContainerExists: func(id string) bool {
			_, err := h.containerManager.Get(id)
			return err == nil
		},
	})
}

func (h *sandboxService) NetworkForSandbox(containerID string) (*SandboxNetwork, error) {
	network, err := h.sandboxNetworking().NetworkForSandbox(containerID)
	if err != nil {
		return nil, err
	}
	return &SandboxNetwork{IP: network.IP, NetNSPath: network.NetNSPath}, nil
}

func (h *sandboxService) sandboxNetworking() *servicenetworking.Coordinator {
	if h == nil {
		return nil
	}
	if h.networking != nil {
		return h.networking
	}
	h.configureNetworking()
	return h.networking
}
