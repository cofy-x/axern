package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSampleRetirementFailsForRemainingAllocation(t *testing.T) {
	root := t.TempDir()
	if err := waitEmptyCgroup(context.Background(), root); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "allocation"), 0700); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitEmptyCgroup(ctx, root); err == nil {
		t.Fatal("retirement accepted a remaining allocation")
	}
}

func TestQualificationMemoryScope(t *testing.T) {
	for _, path := range []string{"", "/", "/sys/fs/cgroup", "/tmp/sandbox"} {
		t.Setenv("AXERN_QUALIFICATION_WORKLOAD_CGROUP", path)
		if _, err := qualificationWorkloadRoot(); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
}

func TestScenarioReadinessRequiresConformanceRetirement(t *testing.T) {
	data, err := os.ReadFile("../../scripts/qualification/network-policy-scenario-in-container.sh")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(data), "conformance_quiescent() {")
	if !ok {
		t.Fatal("missing conformance quiescence gate")
	}
	body, _, ok := strings.Cut(rest, "\n}")
	if !ok {
		t.Fatal("unterminated conformance quiescence gate")
	}
	if !strings.Contains(string(data), "' >/dev/null 2>&1 && conformance_quiescent") {
		t.Fatal("inventory readiness bypasses physical retirement")
	}
	root := t.TempDir()
	check := func() error {
		cmd := exec.Command("bash", "-c", "conformance_quiescent() {"+body+"\n}\nconformance_cgroup_root=\"$1\"\nconformance_quiescent", "test", root)
		return cmd.Run()
	}
	if err := check(); err != nil {
		t.Fatalf("empty domain rejected: %v", err)
	}
	if err := os.Mkdir(filepath.Join(root, "failed-certification"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := check(); err == nil {
		t.Fatal("failed cleanup accepted as quiescent")
	}
}
