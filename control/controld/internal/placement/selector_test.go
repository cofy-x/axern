package placement

import (
	"context"
	"strings"
	"testing"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/status"
)

func TestSelectCandidatesNoEligibleErrorIncludesResourceRequestAndReasons(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	summary := readySummary(now)
	summary.Allocatable = &commonv1.ResourceQuantity{
		CpuMilli:    1000,
		MemoryBytes: 1024,
	}
	setTestMemoryCapacity(summary, 1024)
	summary.Resources.AxnodedCommittedMilli = 900
	summary.Resources.AxnodedCommittedBytes = 900

	registry := nodekernel.NewRegistry()
	registry.Replace([]*nodekernel.Record{
		record("node-a", []string{"runsc"}, summary, now),
	})
	selector := NewSelector(registry, NewEngine(Config{}), func() time.Time { return now }, "runsc")

	_, err := selector.SelectCandidates(context.Background(), &environmentv1.Environment{ID: "env-1"}, &commonv1.ExecutionConfig{
		Resources: &commonv1.ResourceSpec{
			Requests: &commonv1.ResourceQuantity{
				CpuMilli:    200,
				MemoryBytes: 256,
			},
		},
	})
	if err == nil {
		t.Fatal("SelectCandidates returned nil error")
	}
	message := status.Convert(err).Message()
	for _, want := range []string{
		"requested cpu_milli=200 memory_bytes=256",
		"insufficient_cpu",
		"insufficient_memory",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("error message %q does not contain %q", message, want)
		}
	}
	info := errorInfoFromStatus(t, status.Convert(err))
	if info.GetReason() != string(resourcekernel.AdmissionRejectionPlacementCapacity) {
		t.Fatalf("reason = %q, want %q", info.GetReason(), resourcekernel.AdmissionRejectionPlacementCapacity)
	}
	if info.GetMetadata()["diagnostic_code"] != string(resourcekernel.AdmissionDiagnosticPlacementCapacity) {
		t.Fatalf("diagnostic_code = %q", info.GetMetadata()["diagnostic_code"])
	}
}

func TestSelectCandidatesNoEligibleMixedFailuresAreNodeSelection(t *testing.T) {
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	unsupportedLowCapacity := readySummary(now)
	unsupportedLowCapacity.Allocatable = &commonv1.ResourceQuantity{
		CpuMilli:    1000,
		MemoryBytes: 1024,
	}
	setTestMemoryCapacity(unsupportedLowCapacity, 1024)
	unsupportedLowCapacity.Resources.AxnodedCommittedMilli = 900
	unsupportedLowCapacity.Resources.AxnodedCommittedBytes = 900

	registry := nodekernel.NewRegistry()
	registry.Replace([]*nodekernel.Record{
		record("unsupported-low-capacity", []string{"runc"}, unsupportedLowCapacity, now),
	})
	selector := NewSelector(registry, NewEngine(Config{}), func() time.Time { return now }, "runsc")

	_, err := selector.SelectCandidates(context.Background(), &environmentv1.Environment{ID: "env-1"}, &commonv1.ExecutionConfig{
		Resources: &commonv1.ResourceSpec{
			Requests: &commonv1.ResourceQuantity{
				CpuMilli:    200,
				MemoryBytes: 256,
			},
		},
	})
	if err == nil {
		t.Fatal("SelectCandidates returned nil error")
	}
	info := errorInfoFromStatus(t, status.Convert(err))
	if info.GetReason() != string(resourcekernel.AdmissionRejectionNodeSelection) {
		t.Fatalf("reason = %q, want %q", info.GetReason(), resourcekernel.AdmissionRejectionNodeSelection)
	}
	if info.GetMetadata()["diagnostic_code"] != string(resourcekernel.AdmissionDiagnosticNodeSelection) {
		t.Fatalf("diagnostic_code = %q", info.GetMetadata()["diagnostic_code"])
	}
}

func TestSelectCandidatesNoEligibleCapacityAndSelectionCandidatesAreNodeSelection(t *testing.T) {
	now := time.Date(2026, 5, 13, 7, 0, 0, 0, time.UTC)
	lowCapacity := readySummary(now)
	lowCapacity.Allocatable = &commonv1.ResourceQuantity{
		CpuMilli:    1000,
		MemoryBytes: 1024,
	}
	setTestMemoryCapacity(lowCapacity, 1024)
	lowCapacity.Resources.AxnodedCommittedMilli = 900
	lowCapacity.Resources.AxnodedCommittedBytes = 900

	runtimeUnsupported := readySummary(now)

	registry := nodekernel.NewRegistry()
	registry.Replace([]*nodekernel.Record{
		record("low-capacity", []string{"runsc"}, lowCapacity, now),
		record("runtime-unsupported", []string{"runc"}, runtimeUnsupported, now),
	})
	selector := NewSelector(registry, NewEngine(Config{}), func() time.Time { return now }, "runsc")

	_, err := selector.SelectCandidates(context.Background(), &environmentv1.Environment{ID: "env-1"}, &commonv1.ExecutionConfig{
		Resources: &commonv1.ResourceSpec{
			Requests: &commonv1.ResourceQuantity{
				CpuMilli:    200,
				MemoryBytes: 256,
			},
		},
	})
	if err == nil {
		t.Fatal("SelectCandidates returned nil error")
	}
	info := errorInfoFromStatus(t, status.Convert(err))
	if info.GetReason() != string(resourcekernel.AdmissionRejectionNodeSelection) {
		t.Fatalf("reason = %q, want %q", info.GetReason(), resourcekernel.AdmissionRejectionNodeSelection)
	}
	if info.GetMetadata()["diagnostic_code"] != string(resourcekernel.AdmissionDiagnosticNodeSelection) {
		t.Fatalf("diagnostic_code = %q", info.GetMetadata()["diagnostic_code"])
	}
}

func TestSelectCandidatesReturnsRetryableNodesForTransientHealthRejections(t *testing.T) {
	now := time.Date(2026, 5, 13, 6, 0, 0, 0, time.UTC)
	summary := readySummary(now)
	summary.Components.Imagemgr.State = nodev1.ComponentState_COMPONENT_STATE_ERROR
	summary.Components.Imagemgr.Reachable = false

	registry := nodekernel.NewRegistry()
	registry.Replace([]*nodekernel.Record{
		record("node-a", []string{"runsc"}, summary, now),
	})
	selector := NewSelector(registry, NewEngine(Config{}), func() time.Time { return now }, "runsc")
	observer := &selectionRecorder{}
	selector.WithObserver(observer)

	records, err := selector.SelectCandidates(context.Background(), &environmentv1.Environment{ID: "env-1"}, &commonv1.ExecutionConfig{})
	if err != nil {
		t.Fatalf("SelectCandidates() error = %v", err)
	}
	if len(records) != 1 || records[0].NodeID != "node-a" {
		t.Fatalf("retryable records = %#v, want node-a", records)
	}
	if observer.last.Result != SelectionResultRetryable || observer.last.EligibleCount != 1 {
		t.Fatalf("selection observation = %#v, want retryable with one candidate", observer.last)
	}
}

func TestSelectCandidatesDoesNotRetryMixedHardAndTransientRejections(t *testing.T) {
	now := time.Date(2026, 5, 13, 6, 30, 0, 0, time.UTC)
	summary := readySummary(now)
	summary.Components.Imagemgr.State = nodev1.ComponentState_COMPONENT_STATE_ERROR
	summary.Components.Imagemgr.Reachable = false

	registry := nodekernel.NewRegistry()
	registry.Replace([]*nodekernel.Record{
		record("node-a", []string{"runc"}, summary, now),
	})
	selector := NewSelector(registry, NewEngine(Config{}), func() time.Time { return now }, "runsc")

	_, err := selector.SelectCandidates(context.Background(), &environmentv1.Environment{ID: "env-1"}, &commonv1.ExecutionConfig{})
	if err == nil {
		t.Fatal("SelectCandidates() returned nil error")
	}
	message := status.Convert(err).Message()
	for _, want := range []string{"runtime_unsupported", "imagemgr_unavailable"} {
		if !strings.Contains(message, want) {
			t.Fatalf("error message %q does not contain %q", message, want)
		}
	}
}

func errorInfoFromStatus(t *testing.T, st *status.Status) *errdetails.ErrorInfo {
	t.Helper()
	for _, detail := range st.Details() {
		if info, ok := detail.(*errdetails.ErrorInfo); ok {
			return info
		}
	}
	t.Fatal("missing ErrorInfo")
	return nil
}

type selectionRecorder struct {
	last SelectionObservation
}

func (r *selectionRecorder) RecordSelection(_ context.Context, obs SelectionObservation) {
	r.last = obs
}
