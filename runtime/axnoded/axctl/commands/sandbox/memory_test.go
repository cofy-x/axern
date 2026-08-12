package sandbox

import (
	"bytes"
	"strings"
	"testing"

	controlnodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

func TestRenderSandboxMemoryIncludesBoundaryUsageAndEnforcement(t *testing.T) {
	var out bytes.Buffer
	renderSandboxMemory(&out, &controlnodev1.AllocationMemoryObservation{
		AllocationID: "allocation-1", Attempt: 2, Revision: 9, Runtime: "runsc",
		RequestBytes: 128, LimitBytes: 256, CurrentBytes: 64, PeakBytes: 192, PeakAvailable: true,
		AnonBytes: 32, FileBytes: 16, ShmemBytes: 8, KernelBytes: 4,
		EventOom: 1, EventOomKill: 1, EventOomGroupKill: 1,
		PsiAvailable: true, PsiSomeAvg10: 0.25, PsiFullAvg10: 0.05, PsiSomeTotalUsec: 10, PsiFullTotalUsec: 2,
		ParentControlsVerified: true, LeafControlsVerified: true, PidRolesVerified: true,
		CgroupIdentity: "mount:11:12", CleanupState: controlnodev1.AllocationMemoryCleanupState_ALLOCATION_MEMORY_CLEANUP_STATE_ASSIGNED,
	})

	got := out.String()
	for _, want := range []string{
		"Allocation: allocation-1 (attempt 2)",
		"Runtime: runsc",
		"Request / Limit: 128 / 256 bytes",
		"peak source: kernel memory.peak",
		"Anon / File / Shmem / Kernel: 32 / 16 / 8 / 4 bytes",
		"Events high/max/oom/oom_kill/group_kill: 0/0/1/1/1",
		"PSI some/full avg10: 0.250 / 0.050 (total usec 10 / 2)",
		"Enforcement parent/leaf/PIDs: true/true/true",
		"Cgroup: mount:11:12 (assigned)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("memory output missing %q:\n%s", want, got)
		}
	}
}

func TestRenderSandboxMemoryMarksUnavailablePSI(t *testing.T) {
	var out bytes.Buffer
	renderSandboxMemory(&out, &controlnodev1.AllocationMemoryObservation{})
	if got := out.String(); !strings.Contains(got, "PSI: unavailable") || !strings.Contains(got, "peak source: sampled current") || strings.Contains(got, "PSI some/full") {
		t.Fatalf("memory output = %q", got)
	}
}
