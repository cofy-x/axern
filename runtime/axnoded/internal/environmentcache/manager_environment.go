package environmentcache

import (
	"context"
	"fmt"
	"time"

	api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/sirupsen/logrus"
)

type environmentReplacement struct {
	environment  *PreparedEnvironment
	rootfs       *RootFS
	retained     bool
	oldRootfsCfg RootfsConfig
	newRootfsCfg RootfsConfig
}

// PrepareEnvironmentResult carries the runtime and attribution data produced while
// adding or reusing a prepared environment.
type PrepareEnvironmentResult struct {
	Environment  *PreparedEnvironment
	Created      bool
	RootfsReport RootfsPrepareReport
}

func (lm *EnvironmentCache) PrepareEnvironment(ctx context.Context, fr *api.ResolvedEnvironment, cfg RootfsConfig) (PrepareEnvironmentResult, error) {
	result := PrepareEnvironmentResult{
		RootfsReport: RootfsPrepareReport{Steps: make([]RootfsStepSample, 0, 6)},
	}
	resolvedCfg, err := lm.mounter.Resolve(cfg)
	if err != nil {
		return result, err
	}
	cfg = resolvedCfg

	lm.environmentMu.RLock()
	if environment, ok := lm.environments[fr.ID]; ok && preparedEnvironmentMatchesResolvedSpec(environment, fr) && environment.RootFS.Config() == cfg {
		lm.environmentMu.RUnlock()
		logrus.Debugf("Prepared environment %v already exists!", fr.ID)
		result.Environment = environment
		return result, nil
	}
	lm.environmentMu.RUnlock()

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

	var replacement environmentReplacement

	lm.environmentMu.Lock()
	if environment, ok := lm.environments[fr.ID]; ok {
		if preparedEnvironmentMatchesResolvedSpec(environment, fr) && environment.RootFS.Config() == cfg {
			lm.environmentMu.Unlock()
			if _, err := rootfs.ReleaseActiveRef(); err != nil {
				return result, err
			}
			logrus.Debugf("Prepared environment %v already exists (added concurrently)!", fr.ID)
			result.Environment = environment
			return result, nil
		}
		if environment.refcnt > 0 {
			lm.deleteEnvironmentIndexLocked(environment)
			environment.superseded = true
			environment.ClearBundleTemplate()
			logrus.WithFields(logrus.Fields{
				"environment_id":  environment.ID,
				"old_rootfs_type": rootfsTypeLabelFromConfig(environment.RootFS.Config()),
				"new_rootfs_type": rootfsTypeLabelFromConfig(cfg),
			}).Info("superseded active prepared environment due to static config drift")
		} else {
			replacement = lm.prepareReplacementLocked(environment, cfg)
		}
	}

	environment := &PreparedEnvironment{
		ID:               fr.ID,
		Argv:             append([]string(nil), fr.Argv...),
		Env:              cloneStringMap(fr.Env),
		Cwd:              fr.Cwd,
		Mounts:           cloneMounts(fr.Mounts),
		ExecutionProfile: cloneOciExecutionProfile(fr.ExecutionProfile),
		Readonly:         fr.Rootfs.Readonly,
		RootFS:           rootfs,
		manager:          lm,
	}
	lm.environments[environment.ID] = environment
	lm.updateRetentionGaugesLocked()
	lm.environmentMu.Unlock()

	lm.executeReplacement(replacement)
	logrus.Debugf("Add prepared environment: %v", environment)
	result.Environment = environment
	result.Created = true
	return result, nil
}

func (lm *EnvironmentCache) prepareReplacementLocked(environment *PreparedEnvironment, newCfg RootfsConfig) environmentReplacement {
	if environment == nil || environment.released {
		return environmentReplacement{}
	}

	lm.deleteEnvironmentIndexLocked(environment)

	replacement := environmentReplacement{
		environment:  environment,
		rootfs:       environment.RootFS,
		retained:     environment.retained,
		oldRootfsCfg: environment.RootFS.Config(),
		newRootfsCfg: newCfg,
	}

	environment.retained = false
	environment.released = true
	environment.idleSince = time.Time{}
	environment.expireAt = time.Time{}
	environment.ClearBundleTemplate()
	return replacement
}

func (lm *EnvironmentCache) executeReplacement(replacement environmentReplacement) {
	if replacement.environment == nil {
		return
	}

	if replacement.rootfs != nil {
		var err error
		if replacement.retained {
			_, err = replacement.rootfs.ReleaseRetainedRef()
		} else {
			_, err = replacement.rootfs.ReleaseActiveRef()
		}
		if err != nil {
			logrus.WithError(err).Warn("release replaced environment rootfs")
		}
	}

	logrus.WithFields(logrus.Fields{
		"environment_id":  replacement.environment.ID,
		"old_rootfs_type": rootfsTypeLabelFromConfig(replacement.oldRootfsCfg),
		"new_rootfs_type": rootfsTypeLabelFromConfig(replacement.newRootfsCfg),
		"was_retained":    replacement.retained,
	}).Info("replaced prepared environment due to static config drift")

	lm.updateRetentionGauges()
}
