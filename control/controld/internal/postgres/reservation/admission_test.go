package reservation

import (
	"fmt"
	"strings"
	"testing"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestRefreshPlacementCandidateRanksOnlyUnreportedReservations(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	record := &nodekernel.Record{
		NodeID:    "node-a",
		UpdatedAt: now,
		Summary: &nodev1.NodeSummary{Resources: &nodev1.ResourcesSummary{
			AxnodedCommittedMilli: 500,
			AxnodedUsedMilli:      125,
			AxnodedCommittedBytes: 512,
			AxnodedUsedBytes:      256,
		}},
	}
	candidate := &placementkernel.Candidate{
		Record: record,
		Evaluation: &nodev1.PlacementCandidate{
			NodeID: "node-a",
			Rank:   &nodev1.PlacementRank{MountedMatch: true},
		},
	}

	refreshed := refreshPlacementCandidate(candidate, record, resourcekernel.Claim{CPUMilli: 700, MemoryBytes: 768}, testAllocationIDs(3), now)
	if got := refreshed.Evaluation.GetRank().GetAxnodedUsedMilli(); got != 325 {
		t.Fatalf("ranked CPU = %d, want actual 125 + unreported 200", got)
	}
	if got := refreshed.Evaluation.GetRank().GetAxnodedUsedBytes(); got != 512 {
		t.Fatalf("ranked memory = %d, want actual 256 + unreported 256", got)
	}
	if got := refreshed.Evaluation.GetRank().GetAxnodedActiveInstances(); got != 3 {
		t.Fatalf("ranked active instances = %d, want 3", got)
	}
	if !refreshed.Evaluation.GetRank().GetMountedMatch() {
		t.Fatal("static placement preference was not preserved")
	}
}

func TestPlacementCandidateRankingBalancesInFlightReservationsWithinPreferenceTier(t *testing.T) {
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	recordA := &nodekernel.Record{NodeID: "node-a", UpdatedAt: now, Summary: &nodev1.NodeSummary{Resources: &nodev1.ResourcesSummary{}}}
	recordB := &nodekernel.Record{NodeID: "node-b", UpdatedAt: now, Summary: &nodev1.NodeSummary{Resources: &nodev1.ResourcesSummary{}}}
	candidate := func(record *nodekernel.Record) *placementkernel.Candidate {
		return &placementkernel.Candidate{
			Record: record,
			Evaluation: &nodev1.PlacementCandidate{
				NodeID: record.NodeID,
				Rank:   &nodev1.PlacementRank{IdlePoolReady: true},
			},
		}
	}

	busy := refreshPlacementCandidate(candidate(recordA), recordA, resourcekernel.Claim{CPUMilli: 500, MemoryBytes: 512}, testAllocationIDs(2), now)
	idle := refreshPlacementCandidate(candidate(recordB), recordB, resourcekernel.Claim{}, nil, now)
	if !placementkernel.CandidateLess(idle, busy) {
		t.Fatalf("idle candidate should rank ahead of candidate with in-flight reservations")
	}

	mounted := candidate(recordA)
	mounted.Evaluation.Rank.MountedMatch = true
	mounted = refreshPlacementCandidate(mounted, recordA, resourcekernel.Claim{CPUMilli: 1000}, testAllocationIDs(4), now)
	if !placementkernel.CandidateLess(mounted, idle) {
		t.Fatalf("mounted locality preference should remain ahead of resource load")
	}
}

func TestReservationRejectionDiagnosticsCapsDetails(t *testing.T) {
	diagnostics := newReservationRejectionDiagnostics(maxReservationRejectionDetails)
	policy := resourcekernel.AdmissionPolicy{CPUOvercommitRatio: 2}
	allocatable := &commonv1.ResourceQuantity{CpuMilli: 1000, MemoryBytes: 1 << 30}
	used := resourcekernel.Claim{CPUMilli: 1900}
	requested := resourcekernel.Claim{CPUMilli: 200}

	for i := 0; i < maxReservationRejectionDetails+2; i++ {
		diagnostics.Add(
			fmt.Sprintf("node-%d", i),
			policy,
			policy.EvaluateFit(allocatable, used, requested),
		)
	}

	if len(diagnostics.details) != maxReservationRejectionDetails {
		t.Fatalf("details len = %d, want %d", len(diagnostics.details), maxReservationRejectionDetails)
	}
	if diagnostics.omitted != 2 {
		t.Fatalf("omitted = %d, want 2", diagnostics.omitted)
	}
	if got := diagnostics.details[0].NodeID; got != "node-0" {
		t.Fatalf("first detail node = %q, want node-0", got)
	}
	if got := diagnostics.details[len(diagnostics.details)-1].NodeID; got != "node-4" {
		t.Fatalf("last detail node = %q, want node-4", got)
	}
	if got := strings.Join(diagnostics.rejectedResources(), ","); got != "cpu" {
		t.Fatalf("rejected resources = %q, want cpu", got)
	}
}

func TestRuntimeSlotEvaluationUsesAggregateCapacity(t *testing.T) {
	summary := &nodev1.NodeSummary{Pools: &nodev1.PoolsSummary{
		RuntimeSlots: &nodev1.PoolState{Capacity: 64},
	}}

	available := evaluateRuntimeSlots(summary, testAllocationIDs(63))
	if !available.Known || !available.Fits || available.Capacity != 64 || available.Available != 1 {
		t.Fatalf("available slot evaluation = %+v", available)
	}
	full := evaluateRuntimeSlots(summary, testAllocationIDs(64))
	if !full.Known || full.Fits || full.Available != 0 {
		t.Fatalf("full slot evaluation = %+v", full)
	}
}

func TestRuntimeSlotEvaluationRejectsMissingAggregateContract(t *testing.T) {
	evaluation := evaluateRuntimeSlots(&nodev1.NodeSummary{Pools: &nodev1.PoolsSummary{}}, nil)
	if evaluation.Known || evaluation.Fits {
		t.Fatalf("missing runtime slot evaluation = %+v, want unknown and rejected", evaluation)
	}
}

func TestReservationDiagnosticsDescribeRuntimeSlotExhaustion(t *testing.T) {
	diagnostics := newReservationRejectionDiagnostics(maxReservationRejectionDetails)
	diagnostics.AddCandidate(
		"node-a",
		resourcekernel.AdmissionPolicy{},
		resourcekernel.FitEvaluation{},
		runtimeSlotEvaluation{Reserved: 64, Occupied: 64, Capacity: 64, Available: 0, Known: true},
	)

	if got := strings.Join(diagnostics.rejectedResources(), ","); got != "runtime_slots" {
		t.Fatalf("rejected resources = %q, want runtime_slots", got)
	}
	if got := diagnostics.Message(); !strings.Contains(got, "runtime_slots requested=1 reserved=64 active=0 pool_using=0 occupied=64 capacity=64 available=0") {
		t.Fatalf("diagnostic message = %q", got)
	}
}

func TestRuntimeSlotEvaluationUsesReportedOccupancyWhenItExceedsReservations(t *testing.T) {
	summary := &nodev1.NodeSummary{
		Components: &nodev1.ComponentsSummary{Axnoded: &nodev1.AxnodedSummary{
			ActiveAllocationIds: testAllocationIDs(63),
		}},
		Pools: &nodev1.PoolsSummary{
			RuntimeSlots: &nodev1.PoolState{Capacity: 64, Using: 64},
		},
	}
	evaluation := evaluateRuntimeSlots(summary, testAllocationIDs(63))
	if evaluation.Fits || evaluation.Reserved != 63 || evaluation.Active != 63 || evaluation.PoolUsing != 64 || evaluation.Occupied != 64 || evaluation.Available != 0 {
		t.Fatalf("runtime slot evaluation = %+v, want node pool usage to block admission", evaluation)
	}
}

func testAllocationIDs(count int) []string {
	ids := make([]string, count)
	for i := range ids {
		ids[i] = fmt.Sprintf("allocation-%d", i)
	}
	return ids
}

func TestReservationRejectionDetailCapturesStructuredResources(t *testing.T) {
	detail := buildReservationRejectionDetail(
		"node-a",
		resourcekernel.AdmissionPolicy{CPUOvercommitRatio: 2},
		resourcekernel.AdmissionPolicy{CPUOvercommitRatio: 2}.EvaluateFit(
			&commonv1.ResourceQuantity{CpuMilli: 1000, MemoryBytes: 512 << 20},
			resourcekernel.Claim{CPUMilli: 1800, MemoryBytes: 400 << 20},
			resourcekernel.Claim{CPUMilli: 300, MemoryBytes: 200 << 20},
		),
	)

	if detail.NodeID != "node-a" {
		t.Fatalf("node id = %q, want node-a", detail.NodeID)
	}
	if detail.CPU == nil {
		t.Fatal("cpu detail is nil")
	}
	if detail.CPU.Requested != 300 || detail.CPU.Used != 1800 || detail.CPU.EffectiveAllocatable != 2000 || detail.CPU.Available != 200 {
		t.Fatalf("cpu detail = %+v, want requested=300 used=1800 effective=2000 available=200", detail.CPU)
	}
	if detail.Memory == nil {
		t.Fatal("memory detail is nil")
	}
	if detail.Memory.Requested != 200<<20 || detail.Memory.Used != 400<<20 || detail.Memory.EffectiveAllocatable != 512<<20 || detail.Memory.Available != 112<<20 {
		t.Fatalf("memory detail = %+v, want requested=200MiB used=400MiB effective=512MiB available=112MiB", detail.Memory)
	}
}

func TestReservationRejectionErrorIncludesStructuredDetails(t *testing.T) {
	policy := resourcekernel.AdmissionPolicy{CPUOvercommitRatio: 2}
	diagnostics := newReservationRejectionDiagnostics(maxReservationRejectionDetails)
	diagnostics.Add(
		"node-a",
		policy,
		policy.EvaluateFit(
			&commonv1.ResourceQuantity{CpuMilli: 1000},
			resourcekernel.Claim{CPUMilli: 1900},
			resourcekernel.Claim{CPUMilli: 200},
		),
	)

	st := grpcstatus.Convert(reservationRejectionError(diagnostics))
	if st.Code() != codes.ResourceExhausted {
		t.Fatalf("code = %v, want ResourceExhausted", st.Code())
	}
	info := errorInfoFromStatus(t, st)
	if info.GetReason() != string(resourcekernel.AdmissionRejectionNodeReservationCapacity) {
		t.Fatalf("reason = %q, want %q", info.GetReason(), resourcekernel.AdmissionRejectionNodeReservationCapacity)
	}
	if info.GetDomain() != resourcekernel.AdmissionErrorDomain {
		t.Fatalf("domain = %q, want %q", info.GetDomain(), resourcekernel.AdmissionErrorDomain)
	}
	if info.GetMetadata()["diagnostic_code"] != string(resourcekernel.AdmissionDiagnosticNodeReservationCapacity) {
		t.Fatalf("diagnostic_code = %q", info.GetMetadata()["diagnostic_code"])
	}
	if info.GetMetadata()["resources"] != "cpu" {
		t.Fatalf("resources = %q, want cpu", info.GetMetadata()["resources"])
	}
}

func TestQuotaRejectionErrorIncludesStructuredDetails(t *testing.T) {
	cpuLimit := int64(1000)
	evaluation := resourcekernel.NamespaceQuotaPolicy{
		CPUMilliLimit: &cpuLimit,
	}.EvaluateFit(
		resourcekernel.Claim{CPUMilli: 900},
		resourcekernel.Claim{CPUMilli: 200},
	)

	err := quotaRejectionError("team-a", evaluation)
	st, ok := grpcstatus.FromError(err)
	if !ok {
		t.Fatalf("error is not a grpc status: %v", err)
	}
	if st.Code() != codes.ResourceExhausted {
		t.Fatalf("code = %v, want ResourceExhausted", st.Code())
	}
	if st.Message() != "namespace quota exceeded: namespace=team-a cpu requested_milli=200 reserved_milli=900 limit_milli=1000 available_milli=100" {
		t.Fatalf("message = %q", st.Message())
	}
	info := errorInfoFromStatus(t, st)
	if info.GetReason() != namespaceQuotaExceededReason {
		t.Fatalf("reason = %q, want %q", info.GetReason(), namespaceQuotaExceededReason)
	}
	if info.GetDomain() != namespaceQuotaErrorDomain {
		t.Fatalf("domain = %q, want %q", info.GetDomain(), namespaceQuotaErrorDomain)
	}
	wantMetadata := map[string]string{
		"namespace":       "team-a",
		"diagnostic_code": string(resourcekernel.AdmissionDiagnosticNamespaceQuotaExceeded),
		"resources":       "cpu",
		"cpu_unit":        "milli",
		"cpu_requested":   "200",
		"cpu_reserved":    "900",
		"cpu_limit":       "1000",
		"cpu_available":   "100",
	}
	for key, want := range wantMetadata {
		if got := info.GetMetadata()[key]; got != want {
			t.Fatalf("metadata[%q] = %q, want %q", key, got, want)
		}
	}
}

func errorInfoFromStatus(t *testing.T, st *grpcstatus.Status) *errdetails.ErrorInfo {
	t.Helper()
	for _, detail := range st.Details() {
		if candidate, ok := detail.(*errdetails.ErrorInfo); ok {
			return candidate
		}
	}
	t.Fatal("missing ErrorInfo detail")
	return nil
}
