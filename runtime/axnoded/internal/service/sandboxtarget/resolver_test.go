package sandboxtarget

import (
	"errors"
	"testing"
	"time"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolverRunningTarget(t *testing.T) {
	handler := runtimetest.NewFakeRuntimeHandler()
	resolver := NewResolver(Options{
		GetContainer: func(id string) (*container.Container, error) {
			return testContainer(id, container.Status{StartedAt: time.Now().Format(time.RFC3339Nano)}), nil
		},
		RunscHandler: handler,
	})

	target, err := resolver.Running("alloc-1")

	require.NoError(t, err)
	assert.Equal(t, "alloc-1", target.ID)
	assert.Same(t, handler, target.Handler)
	assert.Equal(t, map[string]string{"ready": "true"}, target.Labels())
}

func TestResolverRejectsInvalidContainer(t *testing.T) {
	resolver := NewResolver(Options{
		GetContainer: func(string) (*container.Container, error) {
			return &container.Container{}, nil
		},
		RunscHandler: runtimetest.NewFakeRuntimeHandler(),
	})

	_, err := resolver.Container("alloc-1")

	assert.ErrorIs(t, err, errord.ErrInvalidContainer)
}

func TestResolverRejectsStoppedContainer(t *testing.T) {
	resolver := NewResolver(Options{
		GetContainer: func(id string) (*container.Container, error) {
			return testContainer(id, container.Status{StartedAt: "0"}), nil
		},
		RunscHandler: runtimetest.NewFakeRuntimeHandler(),
	})

	_, err := resolver.Running("alloc-1")

	assert.ErrorIs(t, err, errord.ErrFailedPrecondition)
}

func TestResolverRejectsNilRuntimeHandler(t *testing.T) {
	resolver := NewResolver(Options{
		GetContainer: func(id string) (*container.Container, error) {
			return testContainer(id, container.Status{StartedAt: time.Now().Format(time.RFC3339Nano)}), nil
		},
	})

	_, err := resolver.Container("alloc-1")

	assert.ErrorIs(t, err, errord.ErrInvalidContainer)
}

func TestResolverExecDirectCapability(t *testing.T) {
	resolver := NewResolver(Options{
		GetContainer: func(id string) (*container.Container, error) {
			return testContainer(id, container.Status{StartedAt: time.Now().Format(time.RFC3339Nano)}), nil
		},
		RunscHandler: func() *runtimetest.FakeRuntimeHandler {
			handler := runtimetest.NewFakeRuntimeHandler()
			handler.RuntimeCapabilities.CanExecDirect = false
			return handler
		}(),
	})

	_, err := resolver.ExecDirect("alloc-1")

	assert.ErrorIs(t, err, errord.ErrNotImplemented)
}

func TestResolverPropagatesLookupErrors(t *testing.T) {
	lookupErr := errors.New("lookup failed")
	resolver := NewResolver(Options{
		GetContainer: func(string) (*container.Container, error) {
			return nil, lookupErr
		},
		RunscHandler: runtimetest.NewFakeRuntimeHandler(),
	})

	_, err := resolver.Container("alloc-1")

	assert.ErrorIs(t, err, lookupErr)
}

func testContainer(id string, status container.Status) *container.Container {
	return &container.Container{
		Metadata: &runtime.ContainerMetadata{
			Labels: map[string]string{"ready": "true"},
		},
		Status: fixedStatus{status: status},
	}
}

type fixedStatus struct {
	status container.Status
}

func (s fixedStatus) Get() container.Status {
	return s.status
}

func (s fixedStatus) UpdateSync(container.UpdateFunc) error {
	return nil
}

func (s fixedStatus) Update(container.UpdateFunc) error {
	return nil
}

func (s fixedStatus) Delete() error {
	return nil
}
