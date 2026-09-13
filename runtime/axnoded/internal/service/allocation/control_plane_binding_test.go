package allocation

import (
	"context"
	"strings"
	"testing"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestControlPlaneAllocationBindingIsIndependentDurableAuthority(t *testing.T) {
	store := storetest.NewMockStore()
	handler := runtimetest.NewFakeSandboxRuntime()
	handler.RuntimeName = "runsc"
	first := newTestAllocationControllerWithStore(t, handler, store)
	digest := "sha256:" + strings.Repeat("a", 64)

	require.NoError(t, first.controller.BindControlPlaneAllocation("alloc-1", "node-a", digest))
	require.NoError(t, first.controller.BindControlPlaneAllocation("alloc-1", "node-a", digest))
	require.ErrorContains(t, first.controller.BindControlPlaneAllocation("alloc-1", "node-b", digest), "conflicts")
	assert.True(t, first.controller.ControlPlaneBindingMatches("alloc-1", "node-a"))
	assert.Empty(t, first.controller.ControlPlaneAllocationIDs(), "a binding alone is not runtime existence")

	first.controller.stateMu.Lock()
	first.controller.allocationStates["alloc-1"] = newAllocationState("alloc-1")
	first.controller.stateMu.Unlock()
	assert.Equal(t, []string{"alloc-1"}, first.controller.ControlPlaneAllocationIDs())

	second := newTestAllocationControllerWithStore(t, handler, store)
	ids, err := second.controller.RestoreControlPlaneBindings()
	require.NoError(t, err)
	assert.Equal(t, map[string]struct{}{"alloc-1": {}}, ids)
	assert.True(t, second.controller.ControlPlaneBindingMatches("alloc-1", "node-a"))

	require.ErrorContains(t, second.controller.ReleaseControlPlaneAllocation("alloc-1", "node-b"), "belongs to node")
	require.NoError(t, second.controller.ReleaseControlPlaneAllocation("alloc-1", "node-a"))
	assert.False(t, second.controller.HasControlPlaneBinding("alloc-1"))
}

func TestControlPlaneDeleteCannotDeleteUnboundNodeLocalExecution(t *testing.T) {
	handler := runtimetest.NewFakeSandboxRuntime()
	handler.RuntimeName = "runsc"
	fixture := newTestAllocationController(t, handler)
	fixture.controller.stateMu.Lock()
	fixture.controller.allocationStates["local-1"] = newAllocationState("local-1")
	fixture.controller.stateMu.Unlock()

	_, err := fixture.controller.DeleteControlPlane(context.Background(), &runtime.DeleteRequest{ID: "local-1"}, "node-a")
	require.ErrorContains(t, err, "has no control-plane binding")
	assert.True(t, fixture.controller.HasAllocation("local-1"))
}
