package allocation

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

func prepareGuestProfileRecord(t *testing.T, controller *Controller, id string) *apipb.AllocationEnforcementManifest {
	t.Helper()
	require.NoError(t, controller.StoreAllocationIntent(id, "node-a", activationFenceDigest, time.Now().Add(time.Minute), nil, nil, nil, nil, nil))
	now := time.Now().UTC()
	manifest := &apipb.AllocationEnforcementManifest{
		BundlePath: "/fake/" + id, CreatedAtUnixNano: now.UnixNano(), ProcessSecurityProfile: wire.ProcessSecurityProfile,
	}
	require.NoError(t, controller.StoreVerifiedEnforcementManifest(id, manifest, nil, now))
	return manifest
}

func readGuestProfileRecord(t *testing.T, store testStateStore, id string) *apipb.AllocationState {
	t.Helper()
	var record apipb.AllocationState
	require.NoError(t, store.GetRecord(config.AllocationStateBucket, id, &record))
	return &record
}

func TestGuestProcessProfileVerifiedRequiresPostStartObservation(t *testing.T) {
	store := storetest.NewMockStore()
	fixture := newTestAllocationControllerWithStore(t, &runtimeSpyHandler{name: "runsc"}, store)
	const id = "alloc-profile-observation"
	manifest := prepareGuestProfileRecord(t, fixture.controller, id)
	require.False(t, fixture.controller.GuestProcessProfileVerified(id), "desired pre-start policy is not an observed guest Ready fact")
	require.False(t, readGuestProfileRecord(t, store, id).GetGuestProcessProfileVerified())

	unlock := fixture.controller.LockAllocationLifecycle(id)
	err := fixture.controller.StoreGuestProcessProfileVerified(id, manifest)
	unlock()
	require.NoError(t, err)
	require.True(t, fixture.controller.GuestProcessProfileVerified(id))
	require.True(t, readGuestProfileRecord(t, store, id).GetGuestProcessProfileVerified())

	// A fresh controller must read the durable observation, rather than infer
	// it from today's policy or the runtime's configured implementation.
	restored := newTestAllocationControllerWithStore(t, &runtimeSpyHandler{name: "runsc"}, store)
	require.False(t, restored.controller.HasAllocation(id))
	require.True(t, restored.controller.GuestProcessProfileVerified(id))
	require.False(t, restored.controller.GuestProcessProfileVerified("unknown-allocation"))
}

func TestGuestProcessProfileVerifiedRejectsUnobservedAuthority(t *testing.T) {
	for _, scenario := range []string{"mismatched-manifest", "legacy-profile", "expired-lease", "terminated", "missing-manifest"} {
		t.Run(scenario, func(t *testing.T) {
			store := storetest.NewMockStore()
			fixture := newTestAllocationControllerWithStore(t, &runtimeSpyHandler{name: "runsc"}, store)
			const id = "alloc-profile-rejected"
			manifest := prepareGuestProfileRecord(t, fixture.controller, id)
			switch scenario {
			case "mismatched-manifest":
				manifest = proto.Clone(manifest).(*apipb.AllocationEnforcementManifest)
				manifest.BundlePath += "-different"
			case "legacy-profile", "expired-lease":
				// Represent an already recovered old record or expired authority;
				// current admission deliberately cannot install these states.
				record := readGuestProfileRecord(t, store, id)
				if scenario == "legacy-profile" {
					record.EnforcementManifest.ProcessSecurityProfile = ""
					manifest = proto.Clone(record.EnforcementManifest).(*apipb.AllocationEnforcementManifest)
				} else {
					record.ExecutionLeaseExpiresAtUnixNano = time.Now().Add(-time.Minute).UnixNano()
				}
				require.NoError(t, store.PutRecord(config.AllocationStateBucket, id, record))
				fixture.controller.stateMu.Lock()
				fixture.controller.allocationStates[id].record = cloneAllocationRecord(record)
				fixture.controller.stateMu.Unlock()
			case "terminated":
				require.NoError(t, fixture.controller.MarkTerminationIntent(id, commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_EXECUTION_LEASE_EXPIRED, "execution authority expired"))
			case "missing-manifest":
				manifest = nil
			}
			before := readGuestProfileRecord(t, store, id)
			unlock := fixture.controller.LockAllocationLifecycle(id)
			err := fixture.controller.StoreGuestProcessProfileVerified(id, manifest)
			unlock()
			require.Error(t, err)
			require.False(t, fixture.controller.GuestProcessProfileVerified(id))
			require.True(t, proto.Equal(before, readGuestProfileRecord(t, store, id)), "rejected observation changed durable ownership")
			fixture.controller.stateMu.RLock()
			inMemory := cloneAllocationRecord(fixture.controller.allocationStates[id].record)
			fixture.controller.stateMu.RUnlock()
			require.True(t, proto.Equal(before, inMemory), "rejected observation changed in-memory ownership")
		})
	}
}

type guestProfileRejectingStore struct {
	testStateStore
	err      error
	rejected int
}

func (s *guestProfileRejectingStore) PutRecord(bucket, key string, value proto.Message) error {
	if record, ok := value.(*apipb.AllocationState); ok && bucket == config.AllocationStateBucket && record.GetGuestProcessProfileVerified() {
		s.rejected++
		return s.err
	}
	return s.testStateStore.PutRecord(bucket, key, value)
}

func TestGuestProcessProfileVerifiedPersistenceFailureKeepsPriorState(t *testing.T) {
	store := &guestProfileRejectingStore{testStateStore: storetest.NewMockStore(), err: errors.New("observation store unavailable")}
	fixture := newTestAllocationControllerWithStore(t, &runtimeSpyHandler{name: "runsc"}, store)
	const id = "alloc-profile-persist-error"
	manifest := prepareGuestProfileRecord(t, fixture.controller, id)
	before := readGuestProfileRecord(t, store, id)
	unlock := fixture.controller.LockAllocationLifecycle(id)
	err := fixture.controller.StoreGuestProcessProfileVerified(id, manifest)
	unlock()
	require.ErrorIs(t, err, store.err)
	require.Equal(t, 1, store.rejected)
	require.False(t, fixture.controller.GuestProcessProfileVerified(id))
	require.True(t, proto.Equal(before, readGuestProfileRecord(t, store, id)))
	fixture.controller.stateMu.RLock()
	inMemory := cloneAllocationRecord(fixture.controller.allocationStates[id].record)
	fixture.controller.stateMu.RUnlock()
	require.True(t, proto.Equal(before, inMemory), "failed persistence published an uncommitted observation")
}

type guestProfileStartRuntime struct {
	*runtimeSpyHandler
	startHook func(contract.HandlerOptions)
	startErr  error
}

func (r *guestProfileStartRuntime) StartPreparedContainer(ctx context.Context, prepared *contract.PreparedContainer, options contract.HandlerOptions) (*apipb.ContainerMetadata, error) {
	r.startHook(options)
	if r.startErr != nil {
		return nil, r.startErr
	}
	return r.runtimeSpyHandler.StartPreparedContainer(ctx, prepared, options)
}

func TestGuestProcessProfileVerifiedFollowsSuccessfulRuntimeStart(t *testing.T) {
	for _, startSucceeds := range []bool{true, false} {
		name := "ready"
		if !startSucceeds {
			name = "start-error"
		}
		t.Run(name, func(t *testing.T) {
			const id = "alloc-profile-start"
			store := storetest.NewMockStore()
			handler := &guestProfileStartRuntime{runtimeSpyHandler: &runtimeSpyHandler{
				name: "runsc",
				waitFunc: func(ctx context.Context, _ contract.HandlerOptions) (contract.Exit, error) {
					<-ctx.Done()
					return contract.Exit{}, ctx.Err()
				},
			}}
			if !startSucceeds {
				handler.startErr = errors.New("guest Ready was not confirmed")
				// Preserve genuine cleanup debt so a failed Start cannot erase
				// the record and accidentally make a false-only assertion pass.
				handler.deleteErrors = []error{errors.New("retain runtime cleanup")}
			}
			fixture := newTestAllocationControllerWithStore(t, handler, store)
			require.NoError(t, fixture.controller.StoreAllocationIntent(id, "node-a", activationFenceDigest, time.Now().Add(time.Minute), nil, nil, nil, nil, nil))
			startCalls := 0
			handler.startHook = func(options contract.HandlerOptions) {
				startCalls++
				require.False(t, fixture.controller.GuestProcessProfileVerified(id))
				require.False(t, readGuestProfileRecord(t, store, id).GetGuestProcessProfileVerified())
				require.Equal(t, wire.ProcessSecurityProfile, options.EnforcementManifest.GetProcessSecurityProfile())
			}
			_, err := fixture.controller.Start(t.Context(), &apipb.StartRequest{AllocationID: id, Environment: testResolvedEnvironment(t, "environment-profile-start")})
			require.Equal(t, 1, startCalls)
			if startSucceeds {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, handler.startErr)
				require.ErrorIs(t, err, errRuntimeCleanupPending)
				require.Equal(t, 1, handler.deleteCalls)
			}
			require.Equal(t, startSucceeds, fixture.controller.GuestProcessProfileVerified(id))
			require.Equal(t, startSucceeds, readGuestProfileRecord(t, store, id).GetGuestProcessProfileVerified())
		})
	}
}
