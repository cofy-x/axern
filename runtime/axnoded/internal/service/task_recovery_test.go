package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

type taskRecoveryRuntime struct {
	contract.SandboxRuntime
	verify func(context.Context, string, *apipb.AllocationEnforcementManifest) error
}

func (r taskRecoveryRuntime) VerifyAllocationTaskLimits(ctx context.Context, id string, manifest *apipb.AllocationEnforcementManifest) error {
	return r.verify(ctx, id, manifest)
}

func TestTaskRecoveryReadsPersistedPolicyBeforeAllocationRestore(t *testing.T) {
	for _, configured := range []int64{0, 512} {
		for _, fail := range []bool{false, true} {
			t.Run(strings.Join([]string{map[bool]string{false: "verified", true: "failure"}[fail], map[int64]string{0: "disabled", 512: "changed"}[configured]}, "/"), func(t *testing.T) {
				want := &apipb.AllocationEnforcementManifest{
					BundlePath: "/containers/allocation", CreatedAtUnixNano: 1, GuestTaskLimit: 256, HostTaskLimit: 1024,
					CgroupPath: "/sandbox/allocation", RuntimeCgroupPath: "/sandbox/allocation/workload",
				}
				called := false
				verificationErr := errors.New("persisted task controls changed")
				handler := taskRecoveryRuntime{SandboxRuntime: runtimetest.NewFakeSandboxRuntime(), verify: func(_ context.Context, id string, got *apipb.AllocationEnforcementManifest) error {
					called = true
					require.Equal(t, "allocation", id)
					require.True(t, proto.Equal(want, got), "recovery must read the stored policy, not current config")
					if fail {
						return verificationErr
					}
					return nil
				}}
				service := newTestService(t, handler)
				service.config.RuntimeConfig.GuestTaskLimit = configured
				service.config.RuntimeConfig.HostTaskLimit = configured * 4
				record := &apipb.AllocationState{AllocationID: "allocation", NodeID: "node-a", AllocationRequestDigest: "sha256:" + strings.Repeat("a", 64), ExecutionLeaseExpiresAtUnixNano: time.Now().Add(time.Minute).UnixNano(), EnforcementManifest: want}
				require.NoError(t, service.store.PutRecord(config.AllocationStateBucket, "allocation", record))
				require.Nil(t, service.allocations.VerifiedEnforcementManifest("allocation"), "in-memory allocation restoration has not happened")
				err := service.verifyRecoveredTaskLimits(t.Context(), runtimeInventory{"allocation": {ID: "allocation", Status: contract.ContainerStatusRunning}})
				if fail {
					require.ErrorIs(t, err, verificationErr)
				} else {
					require.NoError(t, err)
				}
				require.True(t, called)
				var after apipb.AllocationState
				require.NoError(t, service.store.GetRecord(config.AllocationStateBucket, "allocation", &after))
				require.True(t, proto.Equal(record, &after), "recovery verification must not rewrite authority")
			})
		}
	}
}

func TestTaskRecoveryRejectsUnknownLiveStateAndSkipsTerminal(t *testing.T) {
	for _, status := range []contract.ContainerStatus{contract.ContainerStatusUnknown, contract.ContainerStatusExited} {
		t.Run(string(status), func(t *testing.T) {
			handler := taskRecoveryRuntime{SandboxRuntime: runtimetest.NewFakeSandboxRuntime(), verify: func(context.Context, string, *apipb.AllocationEnforcementManifest) error {
				t.Fatal("unknown or terminal runtime must not be verified as a live task domain")
				return nil
			}}
			service := newTestService(t, handler)
			service.config.RuntimeConfig.GuestTaskLimit = 256
			err := service.verifyRecoveredTaskLimits(t.Context(), runtimeInventory{"allocation": {ID: "allocation", Status: status}})
			if status == contract.ContainerStatusUnknown {
				require.ErrorContains(t, err, "uncertain runtime state")
			} else {
				require.NoError(t, err)
			}
		})
	}
}
