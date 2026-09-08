package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
)

func TestAllocationDiagnosticRedactsRawReasons(t *testing.T) {
	response := &privatenodev1.GetAllocationStatusResponse{
		ExitCode: 137, ExitCodeKnown: true, Message: "private-destination",
		CapabilityVerification: &capabilityv1.CapabilityConditionSet{
			Conditions: []*capabilityv1.CapabilityCondition{{
				State:   capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_FAILED,
				Message: "private-destination",
			}},
		},
	}
	data, err := json.Marshal(allocationDiagnostic(8, response))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "private-destination") || !strings.Contains(string(data), "CAPABILITY_CONDITION_STATE_FAILED") {
		t.Fatalf("incorrect diagnostic: %s", data)
	}
	if _, err := json.Marshal(allocationDiagnostic(8, nil)); err != nil {
		t.Fatal(err)
	}
}

func TestScenarioFailurePreservesDaemonLogAndExit(t *testing.T) {
	data, err := os.ReadFile("../../scripts/qualification/network-policy-scenario-in-container.sh")
	if err != nil {
		t.Fatal(err)
	}
	_, rest, ok := strings.Cut(string(data), "finish() {")
	if !ok {
		t.Fatal("missing failure evidence trap")
	}
	body, _, ok := strings.Cut(rest, "\n}")
	if !ok || !strings.Contains(string(data), "trap finish EXIT") {
		t.Fatal("missing failure evidence trap installation")
	}
	root := t.TempDir()
	logPath := filepath.Join(root, "daemon.log")
	if err := os.WriteFile(logPath, []byte(strings.Repeat("x", 2<<20)), 0600); err != nil {
		t.Fatal(err)
	}
	body = strings.ReplaceAll(body, "/var/log/axnoded/axnoded.log", logPath)
	for _, code := range []string{"0", "19"} {
		output := filepath.Join(root, "result-"+code)
		command := exec.Command("bash", "-c", "set -euo pipefail\noutput=\"$1\"\ncleanup() { :; }\nfinish() {"+body+"\n}\ntrap finish EXIT\nexit \"$2\"", "test", output, code)
		err := command.Run()
		if code == "0" {
			if err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(output + ".axnoded.log"); !os.IsNotExist(err) {
				t.Fatal("successful scenario retained raw logs")
			}
			continue
		}
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 19 {
			t.Fatalf("failure exit lost: %v", err)
		}
		info, err := os.Stat(output + ".axnoded.log")
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() != 1<<20 || info.Mode().Perm() != 0600 {
			t.Fatalf("unbounded or public failure evidence: %v", info)
		}
	}
}
