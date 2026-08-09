package langruntime

import (
	"context"
	"fmt"
	"time"

	api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/sirupsen/logrus"
)

type runtimeReplacement struct {
	runtime      *LanguageRuntime
	rootfs       *RootFS
	retained     bool
	oldRootfsCfg RootfsConfig
	newRootfsCfg RootfsConfig
}

// AddLangRuntimeResult carries the runtime and attribution data produced while
// adding or reusing a language runtime.
type AddLangRuntimeResult struct {
	Runtime      *LanguageRuntime
	Created      bool
	RootfsReport RootfsPrepareReport
}

func (lm *LangRTManager) AddLangRuntime(ctx context.Context, fr *api.RuntimeTemplate, cfg RootfsConfig, temporary bool) (AddLangRuntimeResult, error) {
	result := AddLangRuntimeResult{
		RootfsReport: RootfsPrepareReport{Steps: make([]RootfsStepSample, 0, 6)},
	}
	resolvedCfg, err := lm.mounter.Resolve(cfg)
	if err != nil {
		return result, err
	}
	cfg = resolvedCfg

	lm.lrMu.RLock()
	if lr, ok := lm.lrtMap[fr.ID]; ok && languageRuntimeMatchesRuntimeTemplate(lr, fr) && lr.RootFS.Config() == cfg {
		lm.lrMu.RUnlock()
		logrus.Debugf("Language runtime %v already exists!", fr.ID)
		lr.SetTemporary(temporary)
		result.Runtime = lr
		return result, nil
	}
	lm.lrMu.RUnlock()

	rootfs, rootfsReport, err := lm.GetRootfsWithReport(cfg)
	result.RootfsReport.Steps = append(result.RootfsReport.Steps, rootfsReport.Steps...)
	if err != nil {
		return result, err
	}
	activeRefStart := time.Now()
	if err := rootfs.IncActiveRef(); err != nil {
		result.RootfsReport.Record(contract.StartupPhaseRootfsPrepare, contract.StartupStepRootfsActiveRef, activeRefStart)
		return result, fmt.Errorf("failed to get active reference of rootfs %v: %w", rootfs.cfg, err)
	}
	result.RootfsReport.Record(contract.StartupPhaseRootfsPrepare, contract.StartupStepRootfsActiveRef, activeRefStart)

	var replacement runtimeReplacement

	lm.lrMu.Lock()
	if lr, ok := lm.lrtMap[fr.ID]; ok {
		if languageRuntimeMatchesRuntimeTemplate(lr, fr) && lr.RootFS.Config() == cfg {
			lm.lrMu.Unlock()
			rootfs.ReleaseActiveRef()
			logrus.Debugf("Language runtime %v already exists (added concurrently)!", fr.ID)
			lr.SetTemporary(temporary)
			result.Runtime = lr
			return result, nil
		}
		if lr.refcnt > 0 {
			lm.deleteRuntimeIndexLocked(lr)
			lr.superseded = true
			lr.temporary = false
			lr.ClearBundleTemplate()
			logrus.WithFields(logrus.Fields{
				"runtime_id":      lr.ID,
				"old_rootfs_type": rootfsTypeLabelFromConfig(lr.RootFS.Config()),
				"new_rootfs_type": rootfsTypeLabelFromConfig(cfg),
			}).Info("superseded active language runtime due to static config drift")
		} else {
			replacement = lm.prepareReplacementLocked(lr, cfg)
		}
	}

	lr := &LanguageRuntime{
		ID:               fr.ID,
		Command:          append([]string(nil), fr.Command...),
		RuntimeEnvs:      cloneStringMap(fr.RuntimeEnvs),
		Cwd:              fr.Cwd,
		Mounts:           cloneMounts(fr.Mounts),
		ExecutionProfile: cloneRuntimeExecutionProfile(fr.ExecutionProfile),
		Sandbox:          fr.Sandbox,
		Readonly:         fr.Rootfs.Readonly,
		RootFS:           rootfs,
		temporary:        temporary,
		manager:          lm,
	}
	lm.lrtMap[lr.ID] = lr
	lm.updateRetentionGaugesLocked()
	lm.lrMu.Unlock()

	lm.executeReplacement(ctx, replacement)
	logrus.Debugf("Add language runtime: %v", lr)
	result.Runtime = lr
	result.Created = true
	return result, nil
}

func (lm *LangRTManager) prepareReplacementLocked(lr *LanguageRuntime, newCfg RootfsConfig) runtimeReplacement {
	if lr == nil || lr.released {
		return runtimeReplacement{}
	}

	lm.deleteRuntimeIndexLocked(lr)

	replacement := runtimeReplacement{
		runtime:      lr,
		rootfs:       lr.RootFS,
		retained:     lr.retained,
		oldRootfsCfg: lr.RootFS.Config(),
		newRootfsCfg: newCfg,
	}

	lr.retained = false
	lr.released = true
	lr.idleSince = time.Time{}
	lr.expireAt = time.Time{}
	lr.ClearBundleTemplate()
	return replacement
}

func (lm *LangRTManager) executeReplacement(ctx context.Context, replacement runtimeReplacement) {
	if replacement.runtime == nil {
		return
	}

	if replacement.rootfs != nil {
		if replacement.retained {
			replacement.rootfs.ReleaseRetainedRef()
		} else {
			replacement.rootfs.ReleaseActiveRef()
		}
	}

	logrus.WithFields(logrus.Fields{
		"runtime_id":      replacement.runtime.ID,
		"old_rootfs_type": rootfsTypeLabelFromConfig(replacement.oldRootfsCfg),
		"new_rootfs_type": rootfsTypeLabelFromConfig(replacement.newRootfsCfg),
		"was_retained":    replacement.retained,
	}).Info("replaced language runtime due to static config drift")

	lm.updateRetentionGauges()
}
