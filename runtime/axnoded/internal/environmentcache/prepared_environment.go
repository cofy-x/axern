package environmentcache

import (
	"sync"
	"time"

	api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	"github.com/sirupsen/logrus"
)

type PreparedEnvironment struct {
	ID               string
	Argv             []string
	Env              map[string]string
	Cwd              string
	Mounts           []*api.Mount
	ExecutionProfile *catalogv1.OciExecutionProfile
	Readonly         bool
	RootFS           *RootFS
	manager          *EnvironmentCache

	refcnt     int64
	retained   bool
	released   bool
	superseded bool
	idleSince  time.Time
	expireAt   time.Time

	templateMu    sync.Mutex
	template      *runtimeoci.BundleTemplate
	templateReady chan struct{}
}

func (environment *PreparedEnvironment) IncRef() {
	if environment == nil {
		return
	}
	if environment.manager != nil {
		environment.manager.acquire(environment)
		return
	}
	if environment.released {
		logrus.Warningf("attempt to increment released prepared environment %v", environment.ID)
		return
	}
	environment.refcnt++
}

func (environment *PreparedEnvironment) DecRef() {
	if environment == nil {
		return
	}
	if environment.manager != nil {
		environment.manager.release(environment)
		return
	}
	if environment.released {
		return
	}
	environment.refcnt--
	if environment.refcnt < 0 {
		logrus.Warningf("Refcount %v < 0, leak happens.", environment.refcnt)
		environment.refcnt = 0
	}
}

func (environment *PreparedEnvironment) Retained() bool {
	if environment == nil {
		return false
	}
	return environment.retained
}

func (environment *PreparedEnvironment) Released() bool {
	if environment == nil {
		return false
	}
	return environment.released
}

func (environment *PreparedEnvironment) IdleSince() time.Time {
	if environment == nil {
		return time.Time{}
	}
	return environment.idleSince
}

func (environment *PreparedEnvironment) ExpireAt() time.Time {
	if environment == nil {
		return time.Time{}
	}
	return environment.expireAt
}
