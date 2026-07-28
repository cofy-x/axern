package storage

import (
	"context"
	"fmt"
	"sync"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
)

type Manager struct {
	mu        sync.Mutex
	providers map[storagev1.VolumeBackend]Provider
	store     Store
	published map[string][]*runtimevolumev1.PublishedVolume
	health    VolumeHealth
}

func NewManager(store Store, providers ...Provider) (*Manager, error) {
	m := &Manager{
		providers: make(map[storagev1.VolumeBackend]Provider, len(providers)),
		store:     store,
		published: make(map[string][]*runtimevolumev1.PublishedVolume),
	}
	for _, provider := range providers {
		if provider == nil {
			continue
		}
		backend := provider.Backend()
		if backend == storagev1.VolumeBackend_VOLUME_BACKEND_UNSPECIFIED {
			return nil, fmt.Errorf("volume provider backend is required")
		}
		if err := validateProviderCapabilities(provider); err != nil {
			return nil, err
		}
		if _, exists := m.providers[backend]; exists {
			return nil, fmt.Errorf("volume provider for backend %s is duplicated", backend)
		}
		m.providers[backend] = provider
	}
	if store != nil {
		loaded, err := store.Load(context.Background())
		if err != nil {
			return nil, err
		}
		m.published = clonePublishedMap(loaded)
	}
	return m, nil
}

func NewDefaultManager(root, localRoot string) (*Manager, error) {
	return NewManager(NewJSONStore(root), NewLocalProvider(localRoot))
}

func (m *Manager) saveLocked(ctx context.Context) error {
	if m.store == nil {
		return nil
	}
	return m.store.Save(ctx, m.published)
}
