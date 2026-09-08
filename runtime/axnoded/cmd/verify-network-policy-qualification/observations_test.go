package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestWorkloadObservationsPreserveOrderAndCompletion(t *testing.T) {
	for _, complete := range []bool{false, true} {
		want := workloadObservations{
			Method: "sandbox-probe-and-policy-rpc-v1", Complete: complete,
			DNS: []float64{0.3, 0.1, 0.2}, Prepare: []float64{23, 19, 21},
			RuleScale: []ruleObservations{{Rules: 1, Prepare: []float64{22, 20}, Reconcile: []float64{0.2, 0.1}}},
		}
		path := filepath.Join(t.TempDir(), "sample.workload-observations")
		if err := writeWorkloadObservations(path, want); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var got workloadObservations
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %+v, want %+v", got, want)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("permissions = %v", info.Mode().Perm())
		}
	}
}

func TestWorkloadObservationsPropagateWriteFailure(t *testing.T) {
	if err := writeWorkloadObservations(filepath.Join(t.TempDir(), "missing", "sample"), workloadObservations{}); err == nil {
		t.Fatal("missing parent must fail")
	}
}
