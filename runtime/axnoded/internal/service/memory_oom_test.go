package service

import (
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
)

func TestMemoryObservationIndicatesOOMOnlyForKernelEventDelta(t *testing.T) {
	manifest := &apipb.AllocationEnforcementManifest{
		MemoryLimitBytes:               128 << 20,
		InitialMemoryEventOomKill:      3,
		InitialMemoryEventOomGroupKill: 1,
	}
	for _, test := range []struct {
		name   string
		events map[string]uint64
		want   bool
	}{
		{name: "ordinary signal has no event delta", events: map[string]uint64{"oom_kill": 3, "oom_group_kill": 1}},
		{name: "individual oom kill", events: map[string]uint64{"oom_kill": 4, "oom_group_kill": 1}, want: true},
		{name: "group oom kill", events: map[string]uint64{"oom_kill": 3, "oom_group_kill": 2}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := memoryObservationIndicatesOOM(manifest, &hostlinux.CgroupMemoryObservation{Events: test.events}); got != test.want {
				t.Fatalf("memoryObservationIndicatesOOM() = %t, want %t", got, test.want)
			}
		})
	}
}
