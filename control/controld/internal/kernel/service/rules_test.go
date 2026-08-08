package servicekernel

import (
	"testing"
	"time"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
)

func TestNewServiceNormalizesAndPreservesScaleToZero(t *testing.T) {
	now := time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC)
	svc := NewService("  ", " env-1 ", 0, nil, map[string]string{"tier": "api"}, nil, nil, nil, nil, now)
	if svc.GetNamespace() != "default" {
		t.Fatalf("namespace = %q, want default", svc.GetNamespace())
	}
	if svc.GetEnvironmentID() != "env-1" {
		t.Fatalf("environment_id = %q, want env-1", svc.GetEnvironmentID())
	}
	if svc.GetReplicas() != 0 {
		t.Fatalf("replicas = %d, want 0", svc.GetReplicas())
	}
	if svc.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_READY {
		t.Fatalf("status = %v, want ready", svc.GetStatus())
	}
	if svc.GetVersion() != 1 {
		t.Fatalf("version = %d, want 1", svc.GetVersion())
	}
	if svc.GetReadyReplicas() != 0 || svc.GetUnhealthyReplicas() != 0 {
		t.Fatalf("replica health counters = ready:%d unhealthy:%d, want 0/0", svc.GetReadyReplicas(), svc.GetUnhealthyReplicas())
	}
	if svc.GetRolloutPolicy().GetMaxSurge() != 1 || svc.GetRolloutPolicy().GetMaxUnavailable() != 0 {
		t.Fatalf("rollout policy = %+v, want max_surge=1 max_unavailable=0", svc.GetRolloutPolicy())
	}
}

func TestApplyUpdateUsesFieldMask(t *testing.T) {
	base := NewService("ns", "env", 2, &commonv1.ExecutionConfig{Cwd: "/srv"}, map[string]string{"a": "1"}, nil, nil, nil, nil, time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC))
	req := &servicev1.UpdateServiceRequest{
		ServiceID:       base.GetID(),
		ExpectedVersion: base.GetVersion(),
		Replicas:        int32ptr(0),
		Labels:          map[string]string{"b": "2"},
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"replicas"}},
	}
	next, err := ApplyUpdate(base, req, time.Date(2026, 4, 24, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ApplyUpdate() error = %v", err)
	}
	if next.GetReplicas() != 0 {
		t.Fatalf("replicas = %d, want 0", next.GetReplicas())
	}
	if next.GetLabels()["a"] != "1" {
		t.Fatalf("labels changed unexpectedly: %#v", next.GetLabels())
	}
	if next.GetVersion() != base.GetVersion()+1 {
		t.Fatalf("version = %d, want %d", next.GetVersion(), base.GetVersion()+1)
	}
}

func TestApplyUpdateRejectsVersionMismatch(t *testing.T) {
	base := NewService("ns", "env", 1, nil, nil, nil, nil, nil, nil, time.Now().UTC())
	_, err := ApplyUpdate(base, &servicev1.UpdateServiceRequest{
		ServiceID:       base.GetID(),
		ExpectedVersion: base.GetVersion() + 1,
	}, time.Now().UTC())
	if grpcstatus.Code(err) != codes.Aborted {
		t.Fatalf("code = %v, want %v", grpcstatus.Code(err), codes.Aborted)
	}
}

func TestMarkDeletedClonesAndStartsDeletion(t *testing.T) {
	base := NewService("ns", "env", 1, nil, nil, nil, nil, nil, nil, time.Now().UTC())
	next := MarkDeleted(base, servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_RETAIN, time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC))
	if next.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETING {
		t.Fatalf("status = %v, want deleting", next.GetStatus())
	}
	if next.GetVersion() != base.GetVersion()+1 {
		t.Fatalf("version = %d, want %d", next.GetVersion(), base.GetVersion()+1)
	}
	if base.GetStatus() == servicev1.ServiceStatus_SERVICE_STATUS_DELETING {
		t.Fatalf("base mutated unexpectedly")
	}
}

func TestApplyStatusUpdatePreservesDeletingState(t *testing.T) {
	deleted := MarkDeleted(
		NewService("ns", "env", 1, nil, nil, nil, nil, nil, nil, time.Now().UTC()),
		servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_RETAIN,
		time.Date(2026, 4, 24, 12, 0, 0, 0, time.UTC),
	)

	next, changed := ApplyStatusUpdate(
		deleted,
		servicev1.ServiceStatus_SERVICE_STATUS_READY,
		"stale reconcile",
		time.Date(2026, 4, 24, 12, 1, 0, 0, time.UTC),
	)

	if changed {
		t.Fatal("ApplyStatusUpdate() changed a deleting service")
	}
	if next.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETING {
		t.Fatalf("status = %v, want DELETING", next.GetStatus())
	}
	if next.GetVersion() != deleted.GetVersion() {
		t.Fatalf("version = %d, want %d", next.GetVersion(), deleted.GetVersion())
	}
}

func TestApplyDeletionProgressOnlyCompletesAtTerminalPhase(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	service := MarkDeleted(NewService("ns", "env", 0, nil, nil, nil, nil, nil, nil, now), servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_DELETE, now)
	reclaiming := &servicev1.ServiceDeletionStatus{Phase: servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES}
	next := ApplyDeletionProgress(service, reclaiming, now.Add(time.Second))
	if next.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETING {
		t.Fatalf("reclaiming status = %s, want deleting", next.GetStatus())
	}
	complete := &servicev1.ServiceDeletionStatus{Phase: servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_COMPLETE}
	finished := ApplyDeletionProgress(next, complete, now.Add(2*time.Second))
	if finished.GetStatus() != servicev1.ServiceStatus_SERVICE_STATUS_DELETED {
		t.Fatalf("complete status = %s, want deleted", finished.GetStatus())
	}
	if reclaiming.GetPhase() != servicev1.ServiceDeletionPhase_SERVICE_DELETION_PHASE_RECLAIMING_VOLUMES {
		t.Fatal("input deletion status mutated")
	}
}

func TestApplyUpdateRejectsDeletingService(t *testing.T) {
	now := time.Date(2026, 7, 17, 13, 0, 0, 0, time.UTC)
	service := MarkDeleted(NewService("ns", "env", 0, nil, nil, nil, nil, nil, nil, now), servicev1.ServiceVolumeDisposition_SERVICE_VOLUME_DISPOSITION_DELETE, now)
	replicas := int32(1)
	_, err := ApplyUpdate(service, &servicev1.UpdateServiceRequest{ServiceID: service.GetID(), ExpectedVersion: service.GetVersion(), Replicas: &replicas}, now.Add(time.Second))
	if grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("ApplyUpdate() code = %s, want FailedPrecondition", grpcstatus.Code(err))
	}
}

func TestApplyUpdateReplacesRolloutPolicy(t *testing.T) {
	base := NewService("ns", "env", 2, nil, nil, nil, nil, nil, nil, time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC))
	next, err := ApplyUpdate(base, &servicev1.UpdateServiceRequest{
		ServiceID:       base.GetID(),
		ExpectedVersion: base.GetVersion(),
		RolloutPolicy:   &servicev1.ServiceRolloutPolicy{MaxSurge: 2, MaxUnavailable: 1},
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"rollout_policy"}},
	}, time.Date(2026, 4, 24, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ApplyUpdate(rollout_policy) error = %v", err)
	}
	if next.GetRolloutPolicy().GetMaxSurge() != 2 || next.GetRolloutPolicy().GetMaxUnavailable() != 1 {
		t.Fatalf("rollout policy = %+v, want max_surge=2 max_unavailable=1", next.GetRolloutPolicy())
	}
}

func TestApplyUpdateEnvironmentIDUsesFieldMask(t *testing.T) {
	base := NewService("ns", "env-1", 2, nil, nil, nil, nil, nil, nil, time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC))
	next, err := ApplyUpdate(base, &servicev1.UpdateServiceRequest{
		ServiceID:       base.GetID(),
		ExpectedVersion: base.GetVersion(),
		EnvironmentID:   stringptr(" env-2 "),
		UpdateMask:      &fieldmaskpb.FieldMask{Paths: []string{"environment_id"}},
	}, time.Date(2026, 4, 24, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ApplyUpdate(environment_id) error = %v", err)
	}
	if next.GetEnvironmentID() != "env-2" {
		t.Fatalf("environment_id = %q, want env-2", next.GetEnvironmentID())
	}
}

func TestApplyUpdateAutoscalingPolicyUsesFieldMask(t *testing.T) {
	base := NewService("ns", "env", 2, nil, nil, nil, nil, nil, nil, time.Date(2026, 4, 24, 10, 0, 0, 0, time.UTC))
	next, err := ApplyUpdate(base, &servicev1.UpdateServiceRequest{
		ServiceID:       base.GetID(),
		ExpectedVersion: base.GetVersion(),
		AutoscalingPolicy: &servicev1.ServiceAutoscalingPolicy{
			MinReplicas: 1,
			MaxReplicas: 5,
			Schedules: []*servicev1.ServiceAutoscalingSchedule{{
				Name:     "business",
				CronUtc:  "* 9-17 * * 1-5",
				Replicas: 3,
			}},
		},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"autoscaling_policy"}},
	}, time.Date(2026, 4, 24, 11, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ApplyUpdate(autoscaling_policy) error = %v", err)
	}
	if next.GetAutoscalingPolicy() == nil {
		t.Fatal("autoscaling policy = nil, want populated")
	}
	if next.GetAutoscalingPolicy().GetMinReplicas() != 1 || next.GetAutoscalingPolicy().GetMaxReplicas() != 5 {
		t.Fatalf("autoscaling policy = %+v, want min=1 max=5", next.GetAutoscalingPolicy())
	}
	if len(next.GetAutoscalingPolicy().GetSchedules()) != 1 || next.GetAutoscalingPolicy().GetSchedules()[0].GetName() != "business" {
		t.Fatalf("autoscaling schedules = %#v, want business schedule", next.GetAutoscalingPolicy().GetSchedules())
	}
}

func TestValidateAndNormalizeRolloutPolicyRejectsNegativeValues(t *testing.T) {
	if _, err := ValidateAndNormalizeRolloutPolicy(&servicev1.ServiceRolloutPolicy{MaxSurge: -1}); grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("max_surge code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}
	if _, err := ValidateAndNormalizeRolloutPolicy(&servicev1.ServiceRolloutPolicy{MaxUnavailable: -1}); grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("max_unavailable code = %v, want %v", grpcstatus.Code(err), codes.InvalidArgument)
	}
}

func TestMatchFilterHidesDeletedTombstonesUnlessExplicitlyRequested(t *testing.T) {
	deleted := &servicev1.Service{
		Status: servicev1.ServiceStatus_SERVICE_STATUS_DELETED,
		Labels: map[string]string{"axern.io/workflow": "agent"},
	}
	if MatchFilter(deleted, nil) {
		t.Fatal("MatchFilter(deleted, nil) = true, want hidden tombstone")
	}
	if MatchFilter(deleted, &servicev1.ServiceListFilter{Labels: map[string]string{"axern.io/workflow": "agent"}}) {
		t.Fatal("MatchFilter(deleted, label filter) = true, want hidden tombstone")
	}
	if !MatchFilter(deleted, &servicev1.ServiceListFilter{
		Statuses: []servicev1.ServiceStatus{servicev1.ServiceStatus_SERVICE_STATUS_DELETED},
	}) {
		t.Fatal("MatchFilter(deleted, explicit deleted status) = false, want tombstone")
	}
}

func TestRolloutStateHonorsConfiguredBudgets(t *testing.T) {
	svc := &servicev1.Service{
		Replicas:      2,
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/new"}},
		RolloutPolicy: &servicev1.ServiceRolloutPolicy{MaxSurge: 2, MaxUnavailable: 1},
	}
	allocations := []*AllocationRecord{
		{AllocationID: "old-a", Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, Ready: true, Config: &commonv1.ExecutionConfig{Argv: []string{"/bin/old"}}},
		{AllocationID: "old-b", Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, Ready: true, Config: &commonv1.ExecutionConfig{Argv: []string{"/bin/old"}}},
		{AllocationID: "new-a", Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, Ready: true, Config: &commonv1.ExecutionConfig{Argv: []string{"/bin/new"}}},
	}
	stampAllocationDesiredSpecs(t, svc, allocations)
	status := BuildRolloutStatus(svc, allocations)
	if status == nil || !status.GetInProgress() {
		t.Fatalf("rollout status = %+v, want in_progress", status)
	}
	if status.GetCurrentReplicas() != 3 || status.GetUpdatedReadyReplicas() != 1 || status.GetOutdatedReplicas() != 2 {
		t.Fatalf("rollout status = %+v, want current=3 updated_ready=1 outdated=2", status)
	}
}

func TestBuildRolloutStatusReturnsSummaryOnlyWhileInProgress(t *testing.T) {
	service := &servicev1.Service{
		Replicas: 2,
		Config:   &commonv1.ExecutionConfig{Argv: []string{"/bin/new"}},
		RolloutPolicy: &servicev1.ServiceRolloutPolicy{
			MaxSurge:       1,
			MaxUnavailable: 0,
		},
	}
	allocations := []*AllocationRecord{
		{AllocationID: "old-a", Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, Ready: true, Config: &commonv1.ExecutionConfig{Argv: []string{"/bin/old"}}},
		{AllocationID: "new-a", Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, Ready: true, Config: &commonv1.ExecutionConfig{Argv: []string{"/bin/new"}}},
		{AllocationID: "new-b", Status: commonv1.AllocationStatus_ALLOCATION_STATUS_STARTING, Config: &commonv1.ExecutionConfig{Argv: []string{"/bin/new"}}},
	}
	stampAllocationDesiredSpecs(t, service, allocations)
	status := BuildRolloutStatus(service, allocations)
	if status == nil || !status.GetInProgress() {
		t.Fatalf("rollout status = %+v, want in_progress", status)
	}
	if status.GetCurrentReplicas() != 3 || status.GetUpdatedReadyReplicas() != 1 || status.GetOutdatedReplicas() != 1 {
		t.Fatalf("rollout status = %+v, want current=3 updated_ready=1 outdated=1", status)
	}

	service.Config = &commonv1.ExecutionConfig{Argv: []string{"/bin/old"}}
	matching := []*AllocationRecord{
		{AllocationID: "old-a", Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, Ready: true, Config: &commonv1.ExecutionConfig{Argv: []string{"/bin/old"}}},
		{AllocationID: "old-b", Status: commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING, Ready: true, Config: &commonv1.ExecutionConfig{Argv: []string{"/bin/old"}}},
	}
	stampAllocationDesiredSpecs(t, service, matching)
	if status := BuildRolloutStatus(service, matching); status != nil {
		t.Fatalf("rollout status = %+v, want nil when rollout is not in progress", status)
	}
}

func TestBuildRolloutStatusBlockedByDiagnostic(t *testing.T) {
	service := &servicev1.Service{
		Replicas:      1,
		Status:        servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
		Message:       `config.secret_env "TOKEN" references secret "sec-missing"`,
		EnvironmentID: "env-new",
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/new"}},
		RolloutPolicy: &servicev1.ServiceRolloutPolicy{MaxSurge: 1, MaxUnavailable: 0},
	}
	allocations := []*AllocationRecord{
		{
			AllocationID:  "old-a",
			EnvironmentID: "env-old",
			Status:        commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/old"}},
		},
	}
	stampAllocationDesiredSpecs(t, service, allocations)
	status := BuildRolloutStatus(service, allocations)
	if status == nil {
		t.Fatal("BuildRolloutStatus() = nil, want blocked rollout summary")
	}
	if status.GetPhase() != servicev1.ServiceRolloutPhase_SERVICE_ROLLOUT_PHASE_BLOCKED {
		t.Fatalf("phase = %v, want BLOCKED", status.GetPhase())
	}
	if status.GetDiagnosticCode() != commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_SECRET_PROJECTION_ERROR {
		t.Fatalf("diagnostic_code = %v, want SECRET_PROJECTION_ERROR", status.GetDiagnosticCode())
	}
	if status.GetDiagnosticMessage() == "" {
		t.Fatal("diagnostic_message = empty, want failure detail")
	}
}

func TestAllocationOutdatedMatchesEnvironmentDrift(t *testing.T) {
	service := &servicev1.Service{
		EnvironmentID: "env-new",
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/sleep", "60"}},
	}
	old := CloneService(service)
	old.EnvironmentID = "env-old"
	oldDigest, err := DesiredSpecDigest(old)
	if err != nil {
		t.Fatal(err)
	}
	if !AllocationOutdated(oldDigest, service) {
		t.Fatal("allocation unexpectedly considered current when environment id drifted")
	}
	currentDigest, err := DesiredSpecDigest(service)
	if err != nil {
		t.Fatal(err)
	}
	if AllocationOutdated(currentDigest, service) {
		t.Fatal("allocation unexpectedly considered outdated when environment and config matched")
	}
}

func TestAllocationOutdatedUsesDesiredSpecIdentity(t *testing.T) {
	service := &servicev1.Service{
		EnvironmentID: "env-writable",
		Config: &commonv1.ExecutionConfig{
			Argv:      []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{CpuMilli: 500, MemoryBytes: 4 << 30}},
		},
	}
	digest, err := DesiredSpecDigest(service)
	if err != nil {
		t.Fatalf("DesiredSpecDigest() error = %v", err)
	}
	if AllocationOutdated(digest, service) {
		t.Fatal("allocation with matching desired identity unexpectedly considered outdated")
	}
	service.Config.Resources.Requests.CpuMilli++
	if !AllocationOutdated(digest, service) {
		t.Fatal("allocation with a changed desired spec unexpectedly considered current")
	}
}

func TestAllocationWithoutDesiredSpecIdentityIsOutdated(t *testing.T) {
	if !AllocationOutdated("", &servicev1.Service{Config: &commonv1.ExecutionConfig{}}) {
		t.Fatal("allocation without desired identity must be replaced")
	}
}

func stampAllocationDesiredSpecs(t *testing.T, service *servicev1.Service, allocations []*AllocationRecord) {
	t.Helper()
	for _, allocation := range allocations {
		intent := CloneService(service)
		intent.EnvironmentID = allocation.EnvironmentID
		intent.Config = allocation.Config
		intent.ReadinessProbe = allocation.ReadinessProbe
		intent.LivenessProbe = allocation.LivenessProbe
		digest, err := DesiredSpecDigest(intent)
		if err != nil {
			t.Fatalf("DesiredSpecDigest() error = %v", err)
		}
		allocation.DesiredSpecDigest = digest
	}
}

func int32ptr(v int32) *int32 { return &v }

func stringptr(v string) *string { return &v }
