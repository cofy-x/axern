package langruntime

import (
	"fmt"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/sirupsen/logrus"
)

type rootfsEntry struct {
	rootfs *RootFS
	err    error
	ready  chan struct{} // closed when rootfs creation is done
}

type RootfsPrepareReport struct {
	Steps []RootfsStepSample
}

type RootfsStepSample struct {
	Phase    contract.StartupPhase
	Step     contract.StartupStep
	Duration time.Duration
}

func (r *RootfsPrepareReport) Record(phase contract.StartupPhase, step contract.StartupStep, started time.Time) {
	if r == nil || started.IsZero() {
		return
	}
	duration := time.Since(started)
	if duration <= 0 {
		duration = time.Nanosecond
	}
	r.Steps = append(r.Steps, RootfsStepSample{Phase: phase, Step: step, Duration: duration})
}

func (lm *LangRTManager) GetRootfs(cfg RootfsConfig) (*RootFS, error) {
	rootfs, _, err := lm.GetRootfsWithReport(cfg)
	return rootfs, err
}

func (lm *LangRTManager) GetRootfsWithReport(cfg RootfsConfig) (*RootFS, RootfsPrepareReport, error) {
	report := RootfsPrepareReport{Steps: make([]RootfsStepSample, 0, 4)}
	lookupStart := time.Now()
	lm.rfMu.Lock()
	if entry, ok := lm.rootfsMap[cfg]; ok {
		ready := rootfsEntryReady(entry)
		lm.rfMu.Unlock()
		report.Record(contract.StartupPhaseRootfsPrepare, contract.StartupStepRootfsCacheLookup, lookupStart)
		waitStart := time.Now()
		<-entry.ready
		if !ready {
			report.Record(contract.StartupPhaseRootfsPrepare, contract.StartupStepRootfsWait, waitStart)
		}
		return entry.rootfs, report, entry.err
	}

	entry := &rootfsEntry{ready: make(chan struct{})}
	lm.rootfsMap[cfg] = entry
	lm.rfMu.Unlock()
	report.Record(contract.StartupPhaseRootfsPrepare, contract.StartupStepRootfsCacheLookup, lookupStart)

	logrus.Infof("rootfs %v not exists, try to create it", cfg)
	if cfg.LeaseID != "" {
		lm.mountLeaseMu.RLock()
		defer lm.mountLeaseMu.RUnlock()
	}
	rootfs, mountReport, err := NewRootFSWithReport(cfg, lm.mounter, func() {
		lm.rfMu.Lock()
		if lm.rootfsMap[cfg] == entry {
			delete(lm.rootfsMap, cfg)
		}
		lm.rfMu.Unlock()
		if cfg.LeaseID != "" {
			if err := lm.ReconcileMountLeases(); err != nil {
				logrus.WithError(err).WithField("lease_id", cfg.LeaseID).Warn("reconcile mount leases after rootfs release")
			}
		}
	})
	report.Steps = append(report.Steps, mountReport.Steps...)

	lm.rfMu.Lock()
	entry.rootfs = rootfs
	entry.err = err
	close(entry.ready)
	if err != nil {
		if lm.rootfsMap[cfg] == entry {
			delete(lm.rootfsMap, cfg)
		}
	}
	lm.rfMu.Unlock()

	if err != nil {
		return nil, report, fmt.Errorf("failed to create rootfs with config %v: %w", cfg, err)
	}

	return rootfs, report, nil
}

func rootfsEntryReady(entry *rootfsEntry) bool {
	select {
	case <-entry.ready:
		return true
	default:
		return false
	}
}
