package langruntime

import (
	"sync"
	"time"

	api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	"github.com/sirupsen/logrus"
)

type LanguageRuntime struct {
	ID               string
	Command          []string
	RuntimeEnvs      map[string]string
	Cwd              string
	Mounts           []*api.Mount
	ExecutionProfile *catalogv1.RuntimeExecutionProfile
	Readonly         bool
	Sandbox          string
	RootFS           *RootFS
	manager          *LangRTManager

	refcnt     int64
	temporary  bool
	retained   bool
	released   bool
	superseded bool
	idleSince  time.Time
	expireAt   time.Time

	templateMu    sync.Mutex
	template      *runtimeoci.BundleTemplate
	templateReady chan struct{}

	envelopeMu                 sync.Mutex
	executionEnvelope          *ExecutionEnvelope
	executionEnvelopePreparing bool
	executionEnvelopeDisabled  bool
}

func (lr *LanguageRuntime) SetTemporary(temporary bool) {
	if lr == nil {
		return
	}
	if lr.manager != nil {
		lr.manager.setTemporary(lr, temporary)
		return
	}
	lr.temporary = temporary
}

func (lr *LanguageRuntime) IncRef() {
	if lr == nil {
		return
	}
	if lr.manager != nil {
		lr.manager.acquire(lr)
		return
	}
	if lr.released {
		logrus.Warningf("attempt to increment released language runtime %v", lr.ID)
		return
	}
	lr.refcnt++
}

func (lr *LanguageRuntime) DecRef() {
	if lr == nil {
		return
	}
	if lr.manager != nil {
		lr.manager.release(lr)
		return
	}
	if lr.released {
		return
	}
	lr.refcnt--
	if lr.refcnt < 0 {
		logrus.Warningf("Refcount %v < 0, leak happens.", lr.refcnt)
		lr.refcnt = 0
	}
}

func (lr *LanguageRuntime) Temporary() bool {
	if lr == nil {
		return false
	}
	return lr.temporary
}

func (lr *LanguageRuntime) Retained() bool {
	if lr == nil {
		return false
	}
	return lr.retained
}

func (lr *LanguageRuntime) SetExecutionEnvelopeEnabled(enabled bool) *ExecutionEnvelope {
	if lr == nil {
		return nil
	}
	lr.envelopeMu.Lock()
	lr.executionEnvelopeDisabled = !enabled
	var envelope *ExecutionEnvelope
	if !enabled {
		envelope = lr.executionEnvelope
		lr.executionEnvelope = nil
		lr.executionEnvelopePreparing = false
	}
	lr.envelopeMu.Unlock()
	lr.updateExecutionEnvelopeGauges()
	return envelope
}

func (lr *LanguageRuntime) ExecutionEnvelopeEnabled() bool {
	if lr == nil {
		return false
	}
	lr.envelopeMu.Lock()
	defer lr.envelopeMu.Unlock()
	return !lr.executionEnvelopeDisabled
}

func (lr *LanguageRuntime) Released() bool {
	if lr == nil {
		return false
	}
	return lr.released
}

func (lr *LanguageRuntime) IdleSince() time.Time {
	if lr == nil {
		return time.Time{}
	}
	return lr.idleSince
}

func (lr *LanguageRuntime) ExpireAt() time.Time {
	if lr == nil {
		return time.Time{}
	}
	return lr.expireAt
}
