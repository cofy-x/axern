package cgroup

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNilControllerEnabled(t *testing.T) {
	var c *Controller
	if c.Enabled() {
		t.Error("nil controller should not be enabled")
	}
}

func TestNilControllerApply(t *testing.T) {
	var c *Controller
	cmd := exec.Command("echo")
	if c.Apply(cmd) {
		t.Fatal("nil controller should not request direct placement")
	}
}

func TestNilControllerAddPID(t *testing.T) {
	var c *Controller
	if err := c.AddPID(1234); err != nil {
		t.Errorf("nil controller AddPID should be no-op, got: %v", err)
	}
}

func TestNilControllerClose(t *testing.T) {
	var c *Controller
	if err := c.Close(); err != nil {
		t.Errorf("nil controller Close should be no-op, got: %v", err)
	}
}

func TestControllerEnabled(t *testing.T) {
	c := &Controller{cgroupVersion: 1, cgroupDir: "/tmp/test", cgroupFD: -1}
	if !c.Enabled() {
		t.Error("non-nil controller should be enabled")
	}
}

func TestControllerDisableDirectPlacement(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific cgroup controller behavior")
	}
	c := &Controller{cgroupVersion: 2, cgroupDir: "/tmp/test", cgroupFD: 3}
	c.useCgroupFD.Store(true)
	cmd := exec.Command("echo")
	if !c.Apply(cmd) {
		t.Fatal("expected direct placement to be enabled")
	}

	c.DisableDirectPlacement()

	cmd = exec.Command("echo")
	if c.Apply(cmd) {
		t.Fatal("expected direct placement to be disabled")
	}
}

func TestControllerV2AddPIDFallsBackToCgroupProcs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-specific cgroup controller behavior")
	}
	dir := t.TempDir()
	procsPath := filepath.Join(dir, "cgroup.procs")
	if err := os.WriteFile(procsPath, nil, 0644); err != nil {
		t.Fatalf("write cgroup.procs: %v", err)
	}

	c := &Controller{cgroupVersion: 2, cgroupDir: dir, cgroupFD: 3}
	c.useCgroupFD.Store(false)
	if err := c.AddPID(1234); err != nil {
		t.Fatalf("AddPID: %v", err)
	}

	data, err := os.ReadFile(procsPath)
	if err != nil {
		t.Fatalf("read cgroup.procs: %v", err)
	}
	if string(data) != "1234" {
		t.Fatalf("unexpected cgroup.procs contents: %q", string(data))
	}
}
