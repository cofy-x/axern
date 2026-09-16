package environmentcache

import (
	"errors"
	"sync"
	"time"

	api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

type EnvironmentCache struct {
	mounter ImageMounter

	environmentMu sync.RWMutex
	environments  map[string]*PreparedEnvironment
	retainedMap   map[string]*PreparedEnvironment

	rfMu      sync.Mutex
	rootfsMap map[RootfsConfig]*rootfsEntry
	// mountLeaseMu allows independent mounts to proceed in parallel while
	// serializing complete desired-set reconciliation against lease acquisition.
	mountLeaseMu sync.RWMutex

	retentionTTL time.Duration
	retentionMax int

	sweeperMu   sync.Mutex
	sweeperStop chan struct{}
	sweeperDone chan struct{}
}

func (lm *EnvironmentCache) GetPreparedEnvironment(id string) *PreparedEnvironment {
	lm.environmentMu.RLock()
	defer lm.environmentMu.RUnlock()
	return lm.environments[id]
}

// FindReusableEnvironment returns the current runtime only when its static
// template and already-resolved immutable rootfs generation match.
func (lm *EnvironmentCache) FindReusableEnvironment(fr *api.ResolvedEnvironment, cfg RootfsConfig) *PreparedEnvironment {
	if fr == nil {
		return nil
	}

	lm.environmentMu.RLock()
	defer lm.environmentMu.RUnlock()
	environment := lm.environments[fr.GetID()]
	if !preparedEnvironmentMatchesTemplate(environment, fr) || environment.RootFS == nil {
		return nil
	}
	if !rootfsConfigMatchesRequest(environment.RootFS.Config(), cfg) {
		return nil
	}
	return environment
}

func (lm *EnvironmentCache) ResolveRootfsConfig(cfg RootfsConfig) (RootfsConfig, error) {
	return lm.mounter.Resolve(cfg)
}

func (lm *EnvironmentCache) ReconcileMountLeases() error {
	lm.mountLeaseMu.Lock()
	defer lm.mountLeaseMu.Unlock()
	lm.rfMu.Lock()
	leaseIDs := make([]string, 0, len(lm.rootfsMap))
	released := make(map[RootfsConfig]*rootfsEntry)
	for cfg, entry := range lm.rootfsMap {
		if entry != nil && entry.rootfs != nil && entry.rootfs.referencesReleased() {
			released[cfg] = entry
			continue
		}
		if entry != nil && entry.err == nil && cfg.LeaseID != "" {
			leaseIDs = append(leaseIDs, cfg.LeaseID)
		}
	}
	lm.rfMu.Unlock()
	// The complete desired set is the sole release path for managed image leases.
	// Keep released entries until delivery succeeds, so the existing sweeper can
	// retry an unavailable imagemgr without another cleanup queue or lease copy.
	reconcileErr := lm.mounter.Reconcile(leaseIDs)
	err := reconcileErr
	for cfg, entry := range released {
		cleanupErr := reconcileErr
		if cfg.LeaseID == "" {
			cleanupErr = lm.mounter.Umount(cfg)
		}
		if cleanupErr != nil {
			if cfg.LeaseID == "" {
				err = errors.Join(err, cleanupErr)
			}
			continue
		}
		lm.rfMu.Lock()
		if lm.rootfsMap[cfg] == entry {
			delete(lm.rootfsMap, cfg)
		}
		lm.rfMu.Unlock()
	}
	return err
}

func (lm *EnvironmentCache) hasReleasedRootfs() bool {
	lm.rfMu.Lock()
	defer lm.rfMu.Unlock()
	for _, entry := range lm.rootfsMap {
		if entry != nil && entry.rootfs != nil && entry.rootfs.referencesReleased() {
			return true
		}
	}
	return false
}

func (lm *EnvironmentCache) retryReleasedRootfs() error {
	if !lm.hasReleasedRootfs() {
		return nil
	}
	return lm.ReconcileMountLeases()
}

// NewEnvironmentCache creates a new EnvironmentCache.
// If mounter is nil, the default production ImageMounter is used.
func NewEnvironmentCache(mounter ...ImageMounter) *EnvironmentCache {
	var m ImageMounter
	if len(mounter) > 0 && mounter[0] != nil {
		m = mounter[0]
	} else {
		m = NewDefaultMounter(true, "")
	}
	lm := &EnvironmentCache{
		mounter:      m,
		environments: make(map[string]*PreparedEnvironment),
		retainedMap:  make(map[string]*PreparedEnvironment),
		rootfsMap:    make(map[RootfsConfig]*rootfsEntry),
	}
	lm.updateRetentionGauges()
	return lm
}

func (lm *EnvironmentCache) List() []*PreparedEnvironment {
	lm.environmentMu.RLock()
	defer lm.environmentMu.RUnlock()

	environmentList := make([]*PreparedEnvironment, 0, len(lm.environments))
	for _, environment := range lm.environments {
		environmentList = append(environmentList, environment)
	}
	return environmentList
}

func (lm *EnvironmentCache) deleteEnvironmentIndexLocked(environment *PreparedEnvironment) {
	if environment == nil {
		return
	}
	if lm.environments[environment.ID] == environment {
		delete(lm.environments, environment.ID)
	}
	if lm.retainedMap[environment.ID] == environment {
		delete(lm.retainedMap, environment.ID)
	}
}
