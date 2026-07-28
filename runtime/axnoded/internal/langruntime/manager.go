package langruntime

import (
	"sync"
	"time"

	api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

type LangRTManager struct {
	mounter ImageMounter

	lrMu        sync.RWMutex
	lrtMap      map[string]*LanguageRuntime
	retainedMap map[string]*LanguageRuntime

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

func (lm *LangRTManager) GetLangRuntime(id string) *LanguageRuntime {
	lm.lrMu.RLock()
	defer lm.lrMu.RUnlock()
	return lm.lrtMap[id]
}

// FindReusableLangRuntime returns the current runtime when its static template
// and requested rootfs source still match. ImageCacheKey is derived during
// resolution, so an unresolved request may reuse the key already held by the
// mounted runtime without querying imagemgr again.
func (lm *LangRTManager) FindReusableLangRuntime(fr *api.RuntimeTemplate, cfg RootfsConfig) *LanguageRuntime {
	if fr == nil {
		return nil
	}

	lm.lrMu.RLock()
	defer lm.lrMu.RUnlock()
	lr := lm.lrtMap[fr.GetID()]
	if !languageRuntimeMatchesRuntimeTemplate(lr, fr) || lr.RootFS == nil {
		return nil
	}
	if !rootfsConfigMatchesRequest(lr.RootFS.Config(), cfg) {
		return nil
	}
	return lr
}

func (lm *LangRTManager) ResolveRootfsConfig(cfg RootfsConfig) (RootfsConfig, error) {
	return lm.mounter.Resolve(cfg)
}

func (lm *LangRTManager) ReconcileMountLeases() error {
	lm.mountLeaseMu.Lock()
	defer lm.mountLeaseMu.Unlock()
	lm.rfMu.Lock()
	leaseIDs := make([]string, 0, len(lm.rootfsMap))
	for cfg, entry := range lm.rootfsMap {
		if entry != nil && entry.err == nil && cfg.LeaseID != "" {
			leaseIDs = append(leaseIDs, cfg.LeaseID)
		}
	}
	lm.rfMu.Unlock()
	return lm.mounter.Reconcile(leaseIDs)
}

// NewLanguageRuntimeManager creates a new LangRTManager.
// If mounter is nil, the default production ImageMounter is used.
func NewLanguageRuntimeManager(mounter ...ImageMounter) *LangRTManager {
	var m ImageMounter
	if len(mounter) > 0 && mounter[0] != nil {
		m = mounter[0]
	} else {
		m = NewDefaultMounter(true, "")
	}
	lm := &LangRTManager{
		mounter:     m,
		lrtMap:      make(map[string]*LanguageRuntime),
		retainedMap: make(map[string]*LanguageRuntime),
		rootfsMap:   make(map[RootfsConfig]*rootfsEntry),
	}
	lm.updateRetentionGauges()
	lm.updateExecutionEnvelopeGauges()
	return lm
}

func (lm *LangRTManager) List() []*LanguageRuntime {
	lm.lrMu.RLock()
	defer lm.lrMu.RUnlock()

	lrtList := make([]*LanguageRuntime, 0, len(lm.lrtMap))
	for _, lrt := range lm.lrtMap {
		lrtList = append(lrtList, lrt)
	}
	return lrtList
}

func (lm *LangRTManager) deleteRuntimeIndexLocked(lr *LanguageRuntime) {
	if lr == nil {
		return
	}
	if lm.lrtMap[lr.ID] == lr {
		delete(lm.lrtMap, lr.ID)
	}
	if lm.retainedMap[lr.ID] == lr {
		delete(lm.retainedMap, lr.ID)
	}
}
