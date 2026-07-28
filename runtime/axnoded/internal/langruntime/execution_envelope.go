package langruntime

import (
	"context"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

const executionEnvelopeDestroyTimeout = 10 * time.Second

type ExecutionEnvelope struct {
	RuntimeEnvelope *contract.ExecutionEnvelope
	Resource        container.OccupiedResource
	PreparedAt      time.Time
	Destroy         func(context.Context) error
}

func destroyExecutionEnvelope(ctx context.Context, envelope *ExecutionEnvelope) error {
	if envelope == nil || envelope.Destroy == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, executionEnvelopeDestroyTimeout)
	defer cancel()
	return envelope.Destroy(cleanupCtx)
}

func (lr *LanguageRuntime) BeginExecutionEnvelopePrepare() bool {
	if lr == nil {
		return false
	}

	lr.envelopeMu.Lock()
	if lr.executionEnvelopeDisabled || lr.released || !lr.retained || lr.executionEnvelope != nil || lr.executionEnvelopePreparing {
		lr.envelopeMu.Unlock()
		return false
	}
	lr.executionEnvelopePreparing = true
	lr.envelopeMu.Unlock()
	lr.updateExecutionEnvelopeGauges()
	return true
}

func (lr *LanguageRuntime) FinishExecutionEnvelopePrepare(envelope *ExecutionEnvelope) bool {
	if lr == nil {
		return false
	}

	lr.envelopeMu.Lock()
	lr.executionEnvelopePreparing = false
	if envelope != nil && !lr.executionEnvelopeDisabled && !lr.released && lr.retained && lr.executionEnvelope == nil {
		lr.executionEnvelope = envelope
		lr.envelopeMu.Unlock()
		lr.updateExecutionEnvelopeGauges()
		return true
	}
	lr.envelopeMu.Unlock()
	lr.updateExecutionEnvelopeGauges()
	return false
}

func (lr *LanguageRuntime) ClaimExecutionEnvelope() *ExecutionEnvelope {
	if lr == nil {
		return nil
	}

	lr.envelopeMu.Lock()
	envelope := lr.executionEnvelope
	lr.executionEnvelope = nil
	lr.envelopeMu.Unlock()
	lr.updateExecutionEnvelopeGauges()
	return envelope
}

func (lr *LanguageRuntime) ClearExecutionEnvelope() *ExecutionEnvelope {
	if lr == nil {
		return nil
	}

	lr.envelopeMu.Lock()
	defer lr.envelopeMu.Unlock()

	envelope := lr.executionEnvelope
	lr.executionEnvelope = nil
	lr.executionEnvelopePreparing = false
	return envelope
}

func (lr *LanguageRuntime) DiscardExecutionEnvelope() *ExecutionEnvelope {
	envelope := lr.ClearExecutionEnvelope()
	lr.updateExecutionEnvelopeGauges()
	return envelope
}

func (lr *LanguageRuntime) HasReadyExecutionEnvelope() bool {
	if lr == nil {
		return false
	}

	lr.envelopeMu.Lock()
	defer lr.envelopeMu.Unlock()
	return lr.executionEnvelope != nil
}

func (lr *LanguageRuntime) ExecutionEnvelopeState() string {
	if lr == nil {
		return ""
	}

	lr.envelopeMu.Lock()
	defer lr.envelopeMu.Unlock()
	if lr.executionEnvelopeDisabled {
		return ""
	}
	switch {
	case lr.executionEnvelope != nil:
		return executionEnvelopeStateReady
	case lr.executionEnvelopePreparing:
		return executionEnvelopeStatePreparing
	case lr.retained:
		return executionEnvelopeStateRetained
	default:
		return ""
	}
}

func (lr *LanguageRuntime) updateExecutionEnvelopeGauges() {
	if lr == nil || lr.manager == nil {
		return
	}
	lr.manager.updateExecutionEnvelopeGauges()
}

func (lm *LangRTManager) updateExecutionEnvelopeGauges() {
	lm.lrMu.RLock()
	defer lm.lrMu.RUnlock()
	lm.updateExecutionEnvelopeGaugesLocked()
}

const (
	executionEnvelopeStateRetained  = "retained"
	executionEnvelopeStatePreparing = "preparing"
	executionEnvelopeStateReady     = "ready"
)

func (lm *LangRTManager) updateExecutionEnvelopeGaugesLocked() {
	type gaugeKey struct {
		runtimeName string
		rootfsType  string
		state       string
	}
	gauges := map[gaugeKey]float64{}
	runtimes := map[string]struct{}{
		config.RuntimeNameRunsc: {},
		config.RuntimeNameRunc:  {},
	}
	for _, lr := range lm.lrtMap {
		if lr == nil || lr.RootFS == nil {
			continue
		}
		state := lr.ExecutionEnvelopeState()
		runtimeName := lr.Sandbox
		if runtimeName == "" {
			continue
		}
		runtimes[runtimeName] = struct{}{}
		if state == "" {
			continue
		}
		rootfsType := lr.RootFS.RootfsTypeLabel()
		gauges[gaugeKey{runtimeName: runtimeName, rootfsType: rootfsType, state: state}]++
	}

	for runtimeName := range runtimes {
		for _, rootfsType := range []string{
			contract.StartupRootfsTypeLocal,
			contract.StartupRootfsTypeImage,
			contract.StartupRootfsTypeS3,
			contract.StartupRootfsTypeUnknown,
		} {
			for _, state := range []string{
				executionEnvelopeStateRetained,
				executionEnvelopeStatePreparing,
				executionEnvelopeStateReady,
			} {
				metrics.RecordExecutionEnvelopeGauge(
					runtimeName,
					rootfsType,
					state,
					gauges[gaugeKey{runtimeName: runtimeName, rootfsType: rootfsType, state: state}],
				)
			}
		}
	}
}
