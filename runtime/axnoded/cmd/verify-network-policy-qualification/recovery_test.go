package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in native diagnosis without the sandbox workload or full matrix.
func TestRecoveryMeasurementLinuxTruth(t *testing.T) {
	if os.Getenv("AXERN_RECOVERY_MEASUREMENT_TRUTH") != "1" {
		t.Skip("requires privileged Linux and egressd binary")
	}
	cfg := config{
		samples: 20, recoverySamples: 200, operationTimeout: 10 * time.Second, ipFamily: "ipv4",
		recoveryNamespace: "axern-recovery-measurement", egressdBinary: "/usr/local/bin/egressd",
		output: filepath.Join(t.TempDir(), "recovery"),
	}
	values, err := measureRestartConvergence(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != cfg.recoverySamples {
		t.Fatalf("samples=%d", len(values))
	}
	t.Logf("observed recovery milliseconds: %v", values)
	t.Logf("distribution: %+v", makeDistribution(values))
}

func TestRecoveryHealthReadyDoesNotSleep(t *testing.T) {
	calls := 0
	err := waitRecoveryHealth(context.Background(), func(context.Context) (bool, error) {
		calls++
		return true, nil
	})
	if err != nil || calls != 1 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestRecoveryHealthRetriesUnhealthyAndTransportFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	calls := 0
	err := waitRecoveryHealth(ctx, func(context.Context) (bool, error) {
		calls++
		if calls == 1 {
			return false, errors.New("not listening")
		}
		return calls == 3, nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestRecoveryHealthProbeSharesOverallDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	want, _ := ctx.Deadline()
	err := waitRecoveryHealth(ctx, func(probe context.Context) (bool, error) {
		got, _ := probe.Deadline()
		if !got.Equal(want) {
			t.Errorf("probe escaped overall deadline: %s vs %s", got, want)
		}
		<-probe.Done()
		return false, probe.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err=%v", err)
	}
}

func TestRecoveryHealthCancelledDoesNotProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := waitRecoveryHealth(ctx, func(context.Context) (bool, error) {
		t.Fatal("cancelled waiter probed")
		return true, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
}

func TestRecoveryObservationsPreserveOrderAndMethod(t *testing.T) {
	path := filepath.Join(t.TempDir(), "samples")
	if err := writeRecoveryObservations(path, []float64{53, 28}, false); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		Method   string    `json:"method"`
		Interval float64   `json:"probeIntervalMilliseconds"`
		Complete bool      `json:"complete"`
		Values   []float64 `json:"observedMilliseconds"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Method != "client-health-and-recovered-proof-v2" || got.Interval != 1 || got.Complete || len(got.Values) != 2 || got.Values[0] != 53 || got.Values[1] != 28 {
		t.Fatalf("observations=%+v", got)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("permissions=%v", info.Mode())
	}
}
