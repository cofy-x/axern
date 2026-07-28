package resources

import "testing"

func TestCPUMemorySubsystems(t *testing.T) {
	subsystems, err := cpuMemorySubsystems()
	if err != nil {
		t.Fatalf("cpuMemorySubsystems error: %v", err)
	}
	if len(subsystems) == 0 {
		t.Fatal("expected at least one subsystem name")
	}
}
