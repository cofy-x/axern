package dashboard

import (
	"context"
	"testing"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestSummaryAggregatesServicesAndTunnels(t *testing.T) {
	control := New(Clients{
		Service: &fakeServiceClient{services: []*servicev1.Service{
			{ID: "svc-ready", Status: servicev1.ServiceStatus_SERVICE_STATUS_READY},
			{ID: "svc-degraded", Status: servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED},
			{
				ID:      "svc-admission",
				Status:  servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
				Message: "rpc error: code = ResourceExhausted desc = namespace quota exceeded: namespace=team-a",
			},
		}},
		Tunnel: &fakeTunnelClient{sessions: []*tunnelv1.TunnelSession{
			{SessionID: "tun-running", Status: tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING},
			{SessionID: "tun-failed", Status: tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_FAILED},
		}},
		Quota: &fakeQuotaClient{quotas: []*quotav1.NamespaceQuota{
			{Namespace: "team-a", CpuMilliLimit: wrapperspb.Int64(1000), ReservedCpuMilli: 850},
			{Namespace: "team-b", MemoryBytesLimit: wrapperspb.Int64(1 << 30), ReservedMemoryBytes: 900 << 20},
		}},
		AllocationLifecycle: &fakeAdminLifecycleClient{},
		Audit:               &fakeAdminAuditClient{},
		Reliability: &fakeReliabilityClient{health: &adminv1.AdminReliabilityHealth{
			Status: adminv1.AdminReliabilityStatus_ADMIN_RELIABILITY_STATUS_DEGRADED,
			Consistency: &adminv1.ConsistencySnapshot{
				Status: adminv1.ConsistencyStatus_CONSISTENCY_STATUS_INCONSISTENT,
				Counts: &adminv1.ConsistencyCounts{Issues: 2},
				Issues: []*adminv1.ConsistencyIssue{{
					Code:             adminv1.ConsistencyIssueCode_CONSISTENCY_ISSUE_CODE_SERVICE_REFERENCE_ENDED_ALLOCATION,
					Severity:         adminv1.ConsistencyIssueSeverity_CONSISTENCY_ISSUE_SEVERITY_ERROR,
					AllocationID:     "alloc-ended",
					OwnerType:        "service",
					OwnerID:          "svc-degraded",
					RepairOwner:      adminv1.ConsistencyRepairOwner_CONSISTENCY_REPAIR_OWNER_SERVICE_CONTROLLER,
					RepairAction:     adminv1.ConsistencyRepairAction_CONSISTENCY_REPAIR_ACTION_SERVICE_RECONCILE,
					RepairTargetType: adminv1.ConsistencyRepairTargetType_CONSISTENCY_REPAIR_TARGET_TYPE_SERVICE,
					RepairTargetID:   "svc-degraded",
					Detail:           "service still references an ended allocation",
				}},
			},
			AllocationLifecycleRetries:    3,
			DueAllocationLifecycleRetries: 1,
			ReconcileUnhealthyComponents:  1,
			Signals: []*adminv1.AdminReliabilitySignal{{
				Code:    adminv1.AdminReliabilitySignalCode_ADMIN_RELIABILITY_SIGNAL_CODE_CONSISTENCY_ISSUES,
				Message: "control-plane consistency has 2 issue(s)",
			}},
		}},
	})
	got, err := control.Summary(context.Background())
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if got.Services.Total != 3 || got.Services.Ready != 1 || got.Services.Degraded != 2 || got.Services.AdmissionBlocked != 1 {
		t.Fatalf("service summary = %#v", got.Services)
	}
	if got.Tunnels.Total != 2 || got.Tunnels.Running != 1 || got.Tunnels.Failed != 1 {
		t.Fatalf("tunnel summary = %#v", got.Tunnels)
	}
	if got.Quotas.Namespaces != 2 || got.Quotas.CPUConstrained != 1 || got.Quotas.MemoryConstrained != 1 {
		t.Fatalf("quota summary = %#v", got.Quotas)
	}
	if got.Quotas.CPUPressure != 1 || got.Quotas.MemoryPressure != 1 {
		t.Fatalf("quota pressure summary = %#v", got.Quotas)
	}
	if got.Reliability == nil || got.Reliability.Status != "degraded" || got.Reliability.ConsistencyIssues != 2 || got.Reliability.AllocationLifecycleRetries != 3 || len(got.Reliability.Signals) != 1 {
		t.Fatalf("reliability summary = %#v", got.Reliability)
	}
	if len(got.Reliability.Issues) != 1 || got.Reliability.Issues[0].RepairTargetType != "service" || got.Reliability.Issues[0].RepairTargetID != "svc-degraded" {
		t.Fatalf("reliability issues = %#v", got.Reliability.Issues)
	}
}

func TestSummaryOmitsReliabilityWhenClientAbsent(t *testing.T) {
	control := New(Clients{
		Service:             &fakeServiceClient{},
		Tunnel:              &fakeTunnelClient{},
		Quota:               &fakeQuotaClient{},
		AllocationLifecycle: &fakeAdminLifecycleClient{},
		Audit:               &fakeAdminAuditClient{},
	})
	got, err := control.Summary(context.Background())
	if err != nil {
		t.Fatalf("Summary returned error: %v", err)
	}
	if got.Reliability != nil {
		t.Fatalf("Reliability = %#v, want nil", got.Reliability)
	}
}

func TestNewQuotaDTOIncludesUsagePercent(t *testing.T) {
	got := NewQuotaDTO(&quotav1.NamespaceQuota{
		Namespace:           "team-a",
		CpuMilliLimit:       wrapperspb.Int64(1000),
		MemoryBytesLimit:    wrapperspb.Int64(1 << 30),
		ReservedCpuMilli:    850,
		ReservedMemoryBytes: 512 << 20,
	})
	if got.CPUUsagePercent == nil || *got.CPUUsagePercent != 85 {
		t.Fatalf("CPUUsagePercent = %v, want 85", got.CPUUsagePercent)
	}
	if got.MemoryUsagePercent == nil || *got.MemoryUsagePercent != 50 {
		t.Fatalf("MemoryUsagePercent = %v, want 50", got.MemoryUsagePercent)
	}
	unlimited := NewQuotaDTO(&quotav1.NamespaceQuota{ReservedCpuMilli: 100})
	if unlimited.CPUUsagePercent != nil {
		t.Fatalf("unlimited CPUUsagePercent = %v, want nil", unlimited.CPUUsagePercent)
	}
}

func TestQuotaEventsReturnsDashboardDTOs(t *testing.T) {
	control := New(Clients{
		Quota: &fakeQuotaClient{events: []*quotav1.NamespaceQuotaEvent{{
			ID:                   "quotaevt-1",
			Namespace:            "team-a",
			Type:                 quotav1.NamespaceQuotaEventType_NAMESPACE_QUOTA_EVENT_TYPE_ADMISSION_REJECTED,
			WorkloadType:         quotav1.NamespaceQuotaEventWorkloadType_NAMESPACE_QUOTA_EVENT_WORKLOAD_TYPE_SERVICE,
			WorkloadID:           "svc-1",
			Reason:               quotav1.NamespaceQuotaEventReason_NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_CPU,
			RequestedCpuMilli:    700,
			ReservedCpuMilli:     500,
			CpuMilliLimit:        wrapperspb.Int64(1000),
			AvailableCpuMilli:    wrapperspb.Int64(500),
			RequestedMemoryBytes: 1 << 30,
			Message:              "namespace quota exceeded",
		}}},
	})
	got, err := control.QuotaEvents(context.Background(), "team-a", 10)
	if err != nil {
		t.Fatalf("QuotaEvents returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("event count = %d, want 1", len(got))
	}
	event := got[0]
	if event.Type != "admission_rejected" || event.WorkloadType != "service" || event.Reason != "insufficient_cpu" {
		t.Fatalf("event labels = type:%q workload:%q reason:%q", event.Type, event.WorkloadType, event.Reason)
	}
	if event.CPUMilliLimit == nil || *event.CPUMilliLimit != 1000 || event.AvailableCPUMilli == nil || *event.AvailableCPUMilli != 500 {
		t.Fatalf("event CPU limit/available = %#v/%#v", event.CPUMilliLimit, event.AvailableCPUMilli)
	}
}

func TestServiceDetailReturnsDashboardDTO(t *testing.T) {
	control := New(Clients{
		Service: &fakeServiceClient{
			service: &servicev1.Service{
				ID:            "svc-1",
				Namespace:     "default",
				Status:        servicev1.ServiceStatus_SERVICE_STATUS_READY,
				Replicas:      2,
				ReadyReplicas: 1,
				Config: &commonv1.ExecutionConfig{
					RuntimeClass: "runsc",
					Resources: &commonv1.ResourceSpec{
						Requests: &commonv1.ResourceQuantity{CpuMilli: 500, MemoryBytes: 512 << 20},
						Limits:   &commonv1.ResourceQuantity{CpuMilli: 1000, MemoryBytes: 1 << 30},
					},
				},
			},
			replicas: []*servicev1.ServiceReplica{{
				ID:        "alloc-1",
				ServiceID: "svc-1",
				Ready:     true,
				Status:    commonv1.AllocationStatus_ALLOCATION_STATUS_RUNNING,
			}},
			events: []*servicev1.ServiceEvent{{
				ID:        "event-1",
				ServiceID: "svc-1",
				Type:      servicev1.ServiceEventType_SERVICE_EVENT_TYPE_SERVICE_RECOVERED,
			}},
		},
		Tunnel:              &fakeTunnelClient{},
		Quota:               &fakeQuotaClient{},
		AllocationLifecycle: &fakeAdminLifecycleClient{},
		Audit:               &fakeAdminAuditClient{},
	})
	got, err := control.ServiceDetail(context.Background(), "svc-1")
	if err != nil {
		t.Fatalf("ServiceDetail returned error: %v", err)
	}
	if got.Service == nil || got.Service.ID != "svc-1" || got.Service.RuntimeClass != "runsc" || got.Service.Status != "ready" {
		t.Fatalf("service dto = %#v", got.Service)
	}
	if got.Service.Resources == nil || got.Service.Resources.Requests.CPUMilli != 500 || got.Service.Resources.Limits.MemoryBytes != 1<<30 {
		t.Fatalf("service resources = %#v", got.Service.Resources)
	}
	if len(got.Replicas) != 1 || got.Replicas[0].Status != "running" {
		t.Fatalf("replicas = %#v", got.Replicas)
	}
	if len(got.Events) != 1 || got.Events[0].ID != "event-1" || got.Events[0].Type != "service_recovered" {
		t.Fatalf("events = %#v", got.Events)
	}
}

func TestAdminReturnsRetryAndAuditDTOs(t *testing.T) {
	control := New(Clients{
		Service: &fakeServiceClient{},
		Tunnel:  &fakeTunnelClient{},
		Quota:   &fakeQuotaClient{},
		AllocationLifecycle: &fakeAdminLifecycleClient{retries: []*adminv1.AllocationLifecycleRetry{{
			AllocationID:       "alloc-a",
			OwnerID:            "svc-a",
			OwnerType:          adminv1.AllocationLifecycleRetryOwnerType_ALLOCATION_LIFECYCLE_RETRY_OWNER_TYPE_SERVICE,
			Reason:             adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE,
			ReconcileAttempts:  2,
			Due:                true,
			Clearable:          false,
			ClearBlockedReason: "active reservations",
		}}},
		Audit: &fakeAdminAuditClient{events: []*adminv1.AdminAuditEvent{{
			EventID:        "audit-a",
			Operation:      adminv1.AdminAuditOperation_ADMIN_AUDIT_OPERATION_FORCE_ALLOCATION_LIFECYCLE_RETRY,
			TargetType:     adminv1.AdminAuditTargetType_ADMIN_AUDIT_TARGET_TYPE_ALLOCATION,
			TargetID:       "alloc-a",
			OperatorReason: "operator forced retry",
		}}},
	})
	got, err := control.Admin(context.Background())
	if err != nil {
		t.Fatalf("Admin returned error: %v", err)
	}
	if len(got.Retries) != 1 || got.Retries[0].OwnerType != "service" || got.Retries[0].Reason != "create" || !got.Retries[0].Due {
		t.Fatalf("retries = %#v", got.Retries)
	}
	if got.Retries[0].Clearable || got.Retries[0].ClearBlockedReason != "active reservations" {
		t.Fatalf("retry clearability = %#v, want blocked by active reservations", got.Retries[0])
	}
	if len(got.Audit) != 1 || got.Audit[0].Operation != "force_allocation_lifecycle_retry" || got.Audit[0].TargetType != "allocation" {
		t.Fatalf("audit = %#v", got.Audit)
	}
}

func TestAdminRetryActionsReturnDTOs(t *testing.T) {
	lifecycle := &fakeAdminLifecycleClient{
		actionRetry: &adminv1.AllocationLifecycleRetry{
			AllocationID: "alloc-a",
			Reason:       adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE,
			Due:          true,
		},
	}
	control := New(Clients{
		Service:             &fakeServiceClient{},
		Tunnel:              &fakeTunnelClient{},
		Quota:               &fakeQuotaClient{},
		AllocationLifecycle: lifecycle,
		Audit:               &fakeAdminAuditClient{},
	})
	for _, tc := range []struct {
		name string
		run  func() (AdminRetryActionResult, error)
	}{
		{name: "force", run: func() (AdminRetryActionResult, error) {
			return control.ForceAllocationLifecycleRetry(context.Background(), "alloc-a", "create", "checked node")
		}},
		{name: "fail", run: func() (AdminRetryActionResult, error) {
			return control.FailAllocationLifecycleCreateRetry(context.Background(), "alloc-a", "checked reservation")
		}},
		{name: "clear", run: func() (AdminRetryActionResult, error) {
			return control.ClearAllocationLifecycleRetry(context.Background(), "alloc-a", "create", "checked cleanup")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.run()
			if err != nil {
				t.Fatalf("%s returned error: %v", tc.name, err)
			}
			if got.Retry.AllocationID != "alloc-a" || got.Retry.Reason != "create" {
				t.Fatalf("%s result = %#v", tc.name, got.Retry)
			}
		})
	}
}

func TestNewServiceDTOClassifiesCapacityAdmissionBlocked(t *testing.T) {
	got := NewServiceDTO(&servicev1.Service{
		ID:      "svc-cpu",
		Status:  servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
		Message: "no eligible node: requested cpu_milli=500 memory_bytes=4294967296; rejection_reasons=insufficient_cpu",
	})
	if got.DiagnosticCode != "admission_blocked" {
		t.Fatalf("DiagnosticCode = %q, want admission_blocked", got.DiagnosticCode)
	}
	if got.DiagnosticMessage == "" {
		t.Fatal("DiagnosticMessage is empty")
	}
	if got.AdmissionSummary != "node CPU capacity exhausted" {
		t.Fatalf("AdmissionSummary = %q, want node CPU capacity exhausted", got.AdmissionSummary)
	}
}

func TestNewServiceDTODoesNotClassifyNodeSelectionAsAdmissionBlocked(t *testing.T) {
	got := NewServiceDTO(&servicev1.Service{
		ID:      "svc-placement",
		Status:  servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
		Message: "no eligible node: requested cpu_milli=500 memory_bytes=4294967296; rejection_reasons=runtime_unsupported",
	})
	if got.DiagnosticCode == "admission_blocked" {
		t.Fatalf("DiagnosticCode = %q, want non-admission diagnostic", got.DiagnosticCode)
	}
	if got.AdmissionSummary != "" {
		t.Fatalf("AdmissionSummary = %q, want empty", got.AdmissionSummary)
	}
}

func TestNormalizeLimitAndLinks(t *testing.T) {
	if got := NormalizeLimit(0); got != DefaultEventLimit {
		t.Fatalf("NormalizeLimit(0) = %d", got)
	}
	if got := NormalizeLimit(MaxEventLimit + 1); got != MaxEventLimit {
		t.Fatalf("NormalizeLimit(max+1) = %d", got)
	}
	links := BuildLinks(LinksConfig{
		ContextName: "compose",
		ServiceURL:  "http://127.0.0.1:25080",
	})
	if links.ContextName != "compose" || len(links.Links) != 3 {
		t.Fatalf("links = %#v", links)
	}
	if links.Links[0].URL != "http://127.0.0.1:25080/dashboard?token=axern-local-dev" {
		t.Fatalf("gateway link = %q", links.Links[0].URL)
	}
	withoutGateway := BuildLinks(LinksConfig{})
	if len(withoutGateway.Links) != 2 {
		t.Fatalf("links without gateway = %#v", withoutGateway)
	}
}

type fakeServiceClient struct {
	service  *servicev1.Service
	services []*servicev1.Service
	replicas []*servicev1.ServiceReplica
	events   []*servicev1.ServiceEvent
}

func (f *fakeServiceClient) CreateService(context.Context, *servicev1.CreateServiceRequest, ...grpc.CallOption) (*servicev1.CreateServiceResponse, error) {
	return nil, nil
}

func (f *fakeServiceClient) GetService(context.Context, *servicev1.GetServiceRequest, ...grpc.CallOption) (*servicev1.GetServiceResponse, error) {
	return &servicev1.GetServiceResponse{Service: f.service}, nil
}

func (f *fakeServiceClient) ListServices(context.Context, *servicev1.ListServicesRequest, ...grpc.CallOption) (*servicev1.ListServicesResponse, error) {
	return &servicev1.ListServicesResponse{Services: f.services}, nil
}

func (f *fakeServiceClient) UpdateService(context.Context, *servicev1.UpdateServiceRequest, ...grpc.CallOption) (*servicev1.UpdateServiceResponse, error) {
	return nil, nil
}

func (f *fakeServiceClient) DeleteService(context.Context, *servicev1.DeleteServiceRequest, ...grpc.CallOption) (*servicev1.DeleteServiceResponse, error) {
	return nil, nil
}

func (f *fakeServiceClient) ListServiceReplicas(_ context.Context, req *servicev1.ListServiceReplicasRequest, _ ...grpc.CallOption) (*servicev1.ListServiceReplicasResponse, error) {
	out := make([]*servicev1.ServiceReplica, 0, len(f.replicas))
	for _, replica := range f.replicas {
		if replica.GetServiceID() == req.GetServiceID() {
			out = append(out, replica)
		}
	}
	return &servicev1.ListServiceReplicasResponse{Replicas: out}, nil
}

func (f *fakeServiceClient) ListServiceEvents(context.Context, *servicev1.ListServiceEventsRequest, ...grpc.CallOption) (*servicev1.ListServiceEventsResponse, error) {
	return &servicev1.ListServiceEventsResponse{Events: f.events}, nil
}

type fakeTunnelClient struct {
	sessions []*tunnelv1.TunnelSession
	events   []*tunnelv1.TunnelSessionEvent
}

func (f *fakeTunnelClient) CreateTunnelSession(context.Context, *tunnelv1.CreateTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.CreateTunnelSessionResponse, error) {
	return nil, nil
}

func (f *fakeTunnelClient) GetTunnelSession(context.Context, *tunnelv1.GetTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.GetTunnelSessionResponse, error) {
	return &tunnelv1.GetTunnelSessionResponse{}, nil
}

func (f *fakeTunnelClient) ListTunnelSessions(context.Context, *tunnelv1.ListTunnelSessionsRequest, ...grpc.CallOption) (*tunnelv1.ListTunnelSessionsResponse, error) {
	return &tunnelv1.ListTunnelSessionsResponse{Sessions: f.sessions}, nil
}

func (f *fakeTunnelClient) RenewTunnelSession(context.Context, *tunnelv1.RenewTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.RenewTunnelSessionResponse, error) {
	return nil, nil
}

func (f *fakeTunnelClient) RevokeTunnelSession(context.Context, *tunnelv1.RevokeTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.RevokeTunnelSessionResponse, error) {
	return nil, nil
}

func (f *fakeTunnelClient) ListTunnelSessionEvents(context.Context, *tunnelv1.ListTunnelSessionEventsRequest, ...grpc.CallOption) (*tunnelv1.ListTunnelSessionEventsResponse, error) {
	return &tunnelv1.ListTunnelSessionEventsResponse{Events: f.events}, nil
}

func (f *fakeTunnelClient) InspectTunnelSession(context.Context, *tunnelv1.InspectTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.InspectTunnelSessionResponse, error) {
	return &tunnelv1.InspectTunnelSessionResponse{}, nil
}

type fakeQuotaClient struct {
	quotas []*quotav1.NamespaceQuota
	events []*quotav1.NamespaceQuotaEvent
}

func (f *fakeQuotaClient) GetNamespaceQuota(context.Context, *quotav1.GetNamespaceQuotaRequest, ...grpc.CallOption) (*quotav1.GetNamespaceQuotaResponse, error) {
	return &quotav1.GetNamespaceQuotaResponse{}, nil
}

func (f *fakeQuotaClient) ListNamespaceQuotas(context.Context, *quotav1.ListNamespaceQuotasRequest, ...grpc.CallOption) (*quotav1.ListNamespaceQuotasResponse, error) {
	return &quotav1.ListNamespaceQuotasResponse{Quotas: f.quotas}, nil
}

func (f *fakeQuotaClient) ListNamespaceQuotaEvents(context.Context, *quotav1.ListNamespaceQuotaEventsRequest, ...grpc.CallOption) (*quotav1.ListNamespaceQuotaEventsResponse, error) {
	return &quotav1.ListNamespaceQuotaEventsResponse{Events: f.events}, nil
}

func (f *fakeQuotaClient) SetNamespaceQuota(context.Context, *quotav1.SetNamespaceQuotaRequest, ...grpc.CallOption) (*quotav1.SetNamespaceQuotaResponse, error) {
	return &quotav1.SetNamespaceQuotaResponse{}, nil
}

func (f *fakeQuotaClient) UnsetNamespaceQuota(context.Context, *quotav1.UnsetNamespaceQuotaRequest, ...grpc.CallOption) (*quotav1.UnsetNamespaceQuotaResponse, error) {
	return &quotav1.UnsetNamespaceQuotaResponse{}, nil
}

type fakeAdminLifecycleClient struct {
	retries     []*adminv1.AllocationLifecycleRetry
	actionRetry *adminv1.AllocationLifecycleRetry
}

func (f *fakeAdminLifecycleClient) ListAllocationLifecycleRetries(context.Context, *adminv1.ListAllocationLifecycleRetriesRequest, ...grpc.CallOption) (*adminv1.ListAllocationLifecycleRetriesResponse, error) {
	return &adminv1.ListAllocationLifecycleRetriesResponse{Retries: f.retries}, nil
}

func (f *fakeAdminLifecycleClient) ForceAllocationLifecycleRetry(context.Context, *adminv1.ForceAllocationLifecycleRetryRequest, ...grpc.CallOption) (*adminv1.ForceAllocationLifecycleRetryResponse, error) {
	return &adminv1.ForceAllocationLifecycleRetryResponse{Retry: f.actionRetry}, nil
}

func (f *fakeAdminLifecycleClient) FailAllocationLifecycleRetry(context.Context, *adminv1.FailAllocationLifecycleRetryRequest, ...grpc.CallOption) (*adminv1.FailAllocationLifecycleRetryResponse, error) {
	return &adminv1.FailAllocationLifecycleRetryResponse{FailedRetry: f.actionRetry}, nil
}

func (f *fakeAdminLifecycleClient) ClearAllocationLifecycleRetry(context.Context, *adminv1.ClearAllocationLifecycleRetryRequest, ...grpc.CallOption) (*adminv1.ClearAllocationLifecycleRetryResponse, error) {
	return &adminv1.ClearAllocationLifecycleRetryResponse{ClearedRetry: f.actionRetry}, nil
}

type fakeAdminAuditClient struct {
	events []*adminv1.AdminAuditEvent
}

func (f *fakeAdminAuditClient) ListAdminAuditEvents(context.Context, *adminv1.ListAdminAuditEventsRequest, ...grpc.CallOption) (*adminv1.ListAdminAuditEventsResponse, error) {
	return &adminv1.ListAdminAuditEventsResponse{Events: f.events}, nil
}

type fakeReliabilityClient struct {
	health *adminv1.AdminReliabilityHealth
}

func (f *fakeReliabilityClient) CheckConsistency(context.Context, *adminv1.CheckConsistencyRequest, ...grpc.CallOption) (*adminv1.CheckConsistencyResponse, error) {
	if f.health == nil {
		return &adminv1.CheckConsistencyResponse{}, nil
	}
	return &adminv1.CheckConsistencyResponse{Snapshot: f.health.GetConsistency()}, nil
}

func (f *fakeReliabilityClient) GetAdminReliabilityHealth(context.Context, *adminv1.GetAdminReliabilityHealthRequest, ...grpc.CallOption) (*adminv1.GetAdminReliabilityHealthResponse, error) {
	return &adminv1.GetAdminReliabilityHealthResponse{Health: f.health}, nil
}
