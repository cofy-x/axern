package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
)

// Isolates the same one-rule Prepare/Reconcile/Delete sequence as the scale
// measurement. It does not claim to reproduce concurrent sandbox load.
func TestPrepareMeasurementLinuxTruth(t *testing.T) {
	if os.Getenv("AXERN_PREPARE_MEASUREMENT_TRUTH") != "1" {
		t.Skip("requires privileged Linux and egressd")
	}
	namespace := "axern-prepare-diagnostic"
	if out, err := exec.Command("ip", "netns", "add", namespace).CombinedOutput(); err != nil {
		t.Fatalf("create isolated namespace: %v: %s", err, out)
	}
	defer exec.Command("ip", "netns", "del", namespace).Run()
	if out, err := exec.Command("ip", "-n", namespace, "link", "set", "lo", "up").CombinedOutput(); err != nil {
		t.Fatalf("activate loopback: %v: %s", err, out)
	}
	root := t.TempDir()
	socket := filepath.Join(root, "egressd.sock")
	command := exec.Command("ip", "netns", "exec", namespace, "/usr/local/bin/egressd", "-root", filepath.Join(root, "state"), "-socket", socket)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	client, err := waitEgressClient(socket, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var prepareValues, reconcileValues []float64
	for sample := 0; sample < 200; sample++ {
		id := fmt.Sprintf("prepare-diagnostic-%d", sample)
		policy := scalePolicy(1, "ipv4")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		started := time.Now()
		prepared, err := client.Prepare(ctx, id, 1, qualificationSourceIP("ipv4"), policy, 1, nil)
		prepareValues = append(prepareValues, milliseconds(time.Since(started)))
		cancel()
		if err != nil {
			t.Fatalf("prepare sample %d: %v", sample, err)
		}
		active := []*runtimeegressv1.ActiveEgressPolicy{{AllocationID: id, Attempt: 1,
			SandboxIp: prepared.GetSandboxIp(), PolicyDigest: prepared.GetPolicyDigest(),
			ExecutionRevision: prepared.GetExecutionRevision()}}
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		started = time.Now()
		_, err = client.Reconcile(ctx, active)
		reconcileValues = append(reconcileValues, milliseconds(time.Since(started)))
		cancel()
		if err != nil {
			t.Fatalf("reconcile sample %d: %v", sample, err)
		}
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Second)
		err = client.Delete(ctx, id, 1)
		cancel()
		if err != nil {
			t.Fatalf("delete sample %d: %v", sample, err)
		}
	}
	t.Logf("prepare observations milliseconds: %v", prepareValues)
	t.Logf("prepare distribution: %+v", makeDistribution(prepareValues))
	t.Logf("reconcile observations milliseconds: %v", reconcileValues)
	t.Logf("reconcile distribution: %+v", makeDistribution(reconcileValues))
}
