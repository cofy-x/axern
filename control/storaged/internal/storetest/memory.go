package storetest

import (
	"sync"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

type MemoryStore struct {
	mu              sync.Mutex
	classes         map[string]*storagev1.VolumeClass
	claims          map[string]*storagev1.VolumeClaim
	claimTombstones map[string]*storagev1.VolumeClaim
	bindings        map[string]*privatestoragev1.VolumeBinding
	reclaimOwners   map[string]string
	reclaimVersions map[string]int64
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		classes:         make(map[string]*storagev1.VolumeClass),
		claims:          make(map[string]*storagev1.VolumeClaim),
		claimTombstones: make(map[string]*storagev1.VolumeClaim),
		bindings:        make(map[string]*privatestoragev1.VolumeBinding),
		reclaimOwners:   make(map[string]string),
		reclaimVersions: make(map[string]int64),
	}
}

func (s *MemoryStore) ensureLocked() {
	if s.classes == nil {
		s.classes = make(map[string]*storagev1.VolumeClass)
	}
	if s.claims == nil {
		s.claims = make(map[string]*storagev1.VolumeClaim)
	}
	if s.claimTombstones == nil {
		s.claimTombstones = make(map[string]*storagev1.VolumeClaim)
	}
	if s.bindings == nil {
		s.bindings = make(map[string]*privatestoragev1.VolumeBinding)
	}
	if s.reclaimOwners == nil {
		s.reclaimOwners = make(map[string]string)
	}
	if s.reclaimVersions == nil {
		s.reclaimVersions = make(map[string]int64)
	}
}
