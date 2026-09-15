package allocation

import (
	"context"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

func TestCgroupLeaseOwnerKindIsUnforgeableContextState(t *testing.T) {
	if got := cgroupLeaseOwnerKind(context.Background()); got != apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_WORKLOAD {
		t.Fatalf("ordinary owner kind = %s", got)
	}
	ctx := context.WithValue(context.Background(), internalConformanceContextKey{}, true)
	if got := cgroupLeaseOwnerKind(ctx); got != apipb.CgroupLeaseOwnerKind_CGROUP_LEASE_OWNER_KIND_RUNTIME_CONFORMANCE {
		t.Fatalf("internal conformance owner kind = %s", got)
	}
	if got := cgroupCapacityCharge(context.Background(), 123); got != 123 {
		t.Fatalf("workload capacity charge = %d", got)
	}
	if got := cgroupCapacityCharge(ctx, 0); got != config.RuntimeConformanceMemoryMaxBytes {
		t.Fatalf("conformance capacity charge = %d, want aggregate ceiling %d", got, config.RuntimeConformanceMemoryMaxBytes)
	}
}

func assertExactContainerExit(t *testing.T, fixture testAllocationController, containerID string, exitCode int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c, err := fixture.manager.Get(containerID)
		if err == nil {
			status := c.Status.Get()
			if status.ExitCode != nil && *status.ExitCode == exitCode {
				if status.Message != "" {
					t.Fatalf("container exit message = %q, want empty for exact runtime exit", status.Message)
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	c, err := fixture.manager.Get(containerID)
	if err != nil {
		t.Fatalf("Get(%q) after exit deadline: %v", containerID, err)
	}
	t.Fatalf("container status after exit deadline = %+v, want known exit code %d", c.Status.Get(), exitCode)
}
