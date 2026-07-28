package dashboard

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	appdashboard "github.com/cofy-x/axern/apps/cli/internal/application/dashboard"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

func TestHandleIndexInjectsRefresh(t *testing.T) {
	srv := &server{refresh: 9_000_000_000}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Axern Dashboard") || !strings.Contains(body, "refreshMs: 9000") {
		t.Fatalf("index body missing expected content: %s", body)
	}
}

func TestHandleTunnelDoctorRequiresOneSelector(t *testing.T) {
	srv := &server{}
	req := httptest.NewRequest(http.MethodGet, "/api/tunnel-doctor?session_id=s1&service_id=svc1", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "exactly one") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestHandleServiceEventsRejectsInvalidLimit(t *testing.T) {
	srv := &server{}
	req := httptest.NewRequest(http.MethodGet, "/api/services/svc-1/events?limit=oops", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandleServiceReplicasRejectsInvalidView(t *testing.T) {
	srv := &server{}
	req := httptest.NewRequest(http.MethodGet, "/api/services/svc-1/replicas?view=stuck", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "replica view must be current or all") {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestHandleLinksUsesGatewayTargetWhenConfigured(t *testing.T) {
	srv := &server{linksConfig: appdashboard.LinksConfig{
		ContextName: "compose",
		ServiceURL:  "http://127.0.0.1:25080",
	}}
	req := httptest.NewRequest(http.MethodGet, "/api/links", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "http://127.0.0.1:25080/dashboard?token=axern-local-dev") || !strings.Contains(body, "13000") {
		t.Fatalf("body = %s", body)
	}
}

func TestHandleQuotasReturnsDashboardQuotas(t *testing.T) {
	srv := &server{
		dashboard: appdashboard.New(appdashboard.Clients{
			Quota:               &fakeDashboardQuotaClient{quotas: []*quotav1.NamespaceQuota{{Namespace: "team-a"}}},
			AllocationLifecycle: &fakeDashboardAdminLifecycleClient{},
			Audit:               &fakeDashboardAdminAuditClient{},
		}),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/quotas", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"namespace": "team-a"`) {
		t.Fatalf("body = %s", rec.Body.String())
	}
}

func TestHandleQuotaEventsReturnsNamespaceEvents(t *testing.T) {
	quotaClient := &fakeDashboardQuotaClient{events: []*quotav1.NamespaceQuotaEvent{{
		ID:           "quotaevt-1",
		Namespace:    "team-a",
		Type:         quotav1.NamespaceQuotaEventType_NAMESPACE_QUOTA_EVENT_TYPE_ADMISSION_REJECTED,
		WorkloadType: quotav1.NamespaceQuotaEventWorkloadType_NAMESPACE_QUOTA_EVENT_WORKLOAD_TYPE_RUN,
		WorkloadID:   "run-1",
		Reason:       quotav1.NamespaceQuotaEventReason_NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_MEMORY,
	}}}
	srv := &server{
		dashboard: appdashboard.New(appdashboard.Clients{
			Quota:               quotaClient,
			AllocationLifecycle: &fakeDashboardAdminLifecycleClient{},
			Audit:               &fakeDashboardAdminAuditClient{},
		}),
	}
	req := httptest.NewRequest(http.MethodGet, "/api/quotas/team-a/events?limit=10", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"id": "quotaevt-1"`, `"workload_type": "run"`, `"reason": "insufficient_memory"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("body missing %q: %s", want, rec.Body.String())
		}
	}
	if quotaClient.lastEventNamespace != "team-a" || quotaClient.lastEventLimit != 10 {
		t.Fatalf("quota event request = namespace:%q limit:%d", quotaClient.lastEventNamespace, quotaClient.lastEventLimit)
	}
}

func TestDashboardAPISmokeReturnsSummaryServicesAndQuotas(t *testing.T) {
	serviceClient := &fakeDashboardServiceClient{
		services: []*servicev1.Service{{
			ID:        "svc-1",
			Namespace: "team-a",
			Status:    servicev1.ServiceStatus_SERVICE_STATUS_DEGRADED,
			Message:   "namespace quota exceeded: namespace=team-a",
			Config: &commonv1.ExecutionConfig{
				RuntimeClass: "runsc",
				Resources: &commonv1.ResourceSpec{
					Requests: &commonv1.ResourceQuantity{CpuMilli: 500, MemoryBytes: 512 << 20},
				},
			},
		}},
	}
	tunnelClient := &fakeDashboardTunnelClient{
		sessions: []*tunnelv1.TunnelSession{{
			SessionID: "tun-1",
			Status:    tunnelv1.TunnelSessionStatus_TUNNEL_SESSION_STATUS_RUNNING,
		}},
	}
	quotaClient := &fakeDashboardQuotaClient{
		quotas: []*quotav1.NamespaceQuota{{
			Namespace:           "team-a",
			CpuMilliLimit:       wrapperspb.Int64(1000),
			ReservedCpuMilli:    900,
			MemoryBytesLimit:    wrapperspb.Int64(1 << 30),
			ReservedMemoryBytes: 128 << 20,
		}},
	}
	adminLifecycleClient := &fakeDashboardAdminLifecycleClient{
		retries: []*adminv1.AllocationLifecycleRetry{{AllocationID: "alloc-1", Due: true}},
	}
	adminAuditClient := &fakeDashboardAdminAuditClient{
		events: []*adminv1.AdminAuditEvent{{EventID: "audit-1", TargetID: "alloc-1"}},
	}
	reliabilityClient := &fakeDashboardReliabilityClient{health: &adminv1.AdminReliabilityHealth{
		Status:                     adminv1.AdminReliabilityStatus_ADMIN_RELIABILITY_STATUS_DEGRADED,
		AllocationLifecycleRetries: 1,
		Consistency: &adminv1.ConsistencySnapshot{
			Status: adminv1.ConsistencyStatus_CONSISTENCY_STATUS_OK,
			Counts: &adminv1.ConsistencyCounts{},
		},
		Signals: []*adminv1.AdminReliabilitySignal{{
			Code:    adminv1.AdminReliabilitySignalCode_ADMIN_RELIABILITY_SIGNAL_CODE_ALLOCATION_LIFECYCLE_RETRIES,
			Message: "1 allocation lifecycle retry item(s), 1 due",
		}},
	}}
	srv := &server{dashboard: appdashboard.New(appdashboard.Clients{
		Service:             serviceClient,
		Tunnel:              tunnelClient,
		Quota:               quotaClient,
		AllocationLifecycle: adminLifecycleClient,
		Audit:               adminAuditClient,
		Reliability:         reliabilityClient,
	})}

	for _, path := range []string{"/api/summary", "/api/reconcile-health", "/api/services", "/api/quotas", "/api/admin"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		srv.routes().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", path, rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/admin", nil)
	rec := httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin status = %d, body = %s", rec.Code, rec.Body.String())
	}
	for _, want := range []string{`"allocation_id": "alloc-1"`, `"event_id": "audit-1"`} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Fatalf("admin body missing %q: %s", want, rec.Body.String())
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/services/svc-1", nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("service detail status = %d, body = %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, want := range []string{`"resources"`, `"cpu_milli": 500`, `"admission_summary": "namespace quota exceeded"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("service detail body missing %q: %s", want, body)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/api/summary", nil)
	rec = httptest.NewRecorder()
	srv.routes().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("summary status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"reliability"`) || !strings.Contains(rec.Body.String(), `"status": "degraded"`) {
		t.Fatalf("summary body missing reliability: %s", rec.Body.String())
	}
}

func TestHandleAdminRetryActions(t *testing.T) {
	lifecycle := &fakeDashboardAdminLifecycleClient{
		actionRetry: &adminv1.AllocationLifecycleRetry{
			AllocationID: "alloc-1",
			Reason:       adminv1.AllocationLifecycleRetryReason_ALLOCATION_LIFECYCLE_RETRY_REASON_CREATE,
		},
	}
	srv := &server{dashboard: appdashboard.New(appdashboard.Clients{
		AllocationLifecycle: lifecycle,
		Audit:               &fakeDashboardAdminAuditClient{},
	})}
	for _, tc := range []struct {
		name       string
		path       string
		body       string
		wantCalled string
	}{
		{name: "force", path: "/api/admin/allocation-retries/alloc-1/force", body: `{"reason":"create","operator_reason":"checked node recovery"}`, wantCalled: "force"},
		{name: "fail", path: "/api/admin/allocation-retries/alloc-1/fail", body: `{"operator_reason":"checked reservation"}`, wantCalled: "fail"},
		{name: "clear", path: "/api/admin/allocation-retries/alloc-1/clear", body: `{"reason":"create","operator_reason":"checked cleanup"}`, wantCalled: "clear"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lifecycle.called = ""
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			srv.routes().ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
			}
			if lifecycle.called != tc.wantCalled {
				t.Fatalf("called = %q, want %q", lifecycle.called, tc.wantCalled)
			}
			if !strings.Contains(rec.Body.String(), `"allocation_id": "alloc-1"`) {
				t.Fatalf("body = %s", rec.Body.String())
			}
		})
	}
}

func TestHandleAdminRetryActionValidation(t *testing.T) {
	srv := &server{dashboard: appdashboard.New(appdashboard.Clients{
		AllocationLifecycle: &fakeDashboardAdminLifecycleClient{},
		Audit:               &fakeDashboardAdminAuditClient{},
	})}
	for _, tc := range []struct {
		name string
		path string
		body string
		want int
	}{
		{name: "missing operator reason", path: "/api/admin/allocation-retries/alloc-1/force", body: `{"reason":"create"}`, want: http.StatusBadRequest},
		{name: "invalid force reason", path: "/api/admin/allocation-retries/alloc-1/force", body: `{"reason":"other","operator_reason":"checked"}`, want: http.StatusBadRequest},
		{name: "invalid fail reason", path: "/api/admin/allocation-retries/alloc-1/fail", body: `{"reason":"delete","operator_reason":"checked"}`, want: http.StatusBadRequest},
		{name: "unknown action", path: "/api/admin/allocation-retries/alloc-1/retry", body: `{"operator_reason":"checked"}`, want: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			rec := httptest.NewRecorder()
			srv.routes().ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d, body = %s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

type fakeDashboardServiceClient struct {
	services []*servicev1.Service
	replicas []*servicev1.ServiceReplica
	events   []*servicev1.ServiceEvent
}

func (f *fakeDashboardServiceClient) CreateService(context.Context, *servicev1.CreateServiceRequest, ...grpc.CallOption) (*servicev1.CreateServiceResponse, error) {
	return nil, nil
}

func (f *fakeDashboardServiceClient) GetService(_ context.Context, req *servicev1.GetServiceRequest, _ ...grpc.CallOption) (*servicev1.GetServiceResponse, error) {
	for _, service := range f.services {
		if service.GetID() == req.GetServiceID() {
			return &servicev1.GetServiceResponse{Service: service}, nil
		}
	}
	return &servicev1.GetServiceResponse{}, nil
}

func (f *fakeDashboardServiceClient) ListServices(context.Context, *servicev1.ListServicesRequest, ...grpc.CallOption) (*servicev1.ListServicesResponse, error) {
	return &servicev1.ListServicesResponse{Services: f.services}, nil
}

func (f *fakeDashboardServiceClient) UpdateService(context.Context, *servicev1.UpdateServiceRequest, ...grpc.CallOption) (*servicev1.UpdateServiceResponse, error) {
	return nil, nil
}

func (f *fakeDashboardServiceClient) DeleteService(context.Context, *servicev1.DeleteServiceRequest, ...grpc.CallOption) (*servicev1.DeleteServiceResponse, error) {
	return nil, nil
}

func (f *fakeDashboardServiceClient) ListServiceReplicas(context.Context, *servicev1.ListServiceReplicasRequest, ...grpc.CallOption) (*servicev1.ListServiceReplicasResponse, error) {
	return &servicev1.ListServiceReplicasResponse{Replicas: f.replicas}, nil
}

func (f *fakeDashboardServiceClient) ListServiceEvents(context.Context, *servicev1.ListServiceEventsRequest, ...grpc.CallOption) (*servicev1.ListServiceEventsResponse, error) {
	return &servicev1.ListServiceEventsResponse{Events: f.events}, nil
}

type fakeDashboardTunnelClient struct {
	sessions []*tunnelv1.TunnelSession
}

func (f *fakeDashboardTunnelClient) CreateTunnelSession(context.Context, *tunnelv1.CreateTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.CreateTunnelSessionResponse, error) {
	return nil, nil
}

func (f *fakeDashboardTunnelClient) GetTunnelSession(context.Context, *tunnelv1.GetTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.GetTunnelSessionResponse, error) {
	return &tunnelv1.GetTunnelSessionResponse{}, nil
}

func (f *fakeDashboardTunnelClient) ListTunnelSessions(context.Context, *tunnelv1.ListTunnelSessionsRequest, ...grpc.CallOption) (*tunnelv1.ListTunnelSessionsResponse, error) {
	return &tunnelv1.ListTunnelSessionsResponse{Sessions: f.sessions}, nil
}

func (f *fakeDashboardTunnelClient) RenewTunnelSession(context.Context, *tunnelv1.RenewTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.RenewTunnelSessionResponse, error) {
	return nil, nil
}

func (f *fakeDashboardTunnelClient) RevokeTunnelSession(context.Context, *tunnelv1.RevokeTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.RevokeTunnelSessionResponse, error) {
	return nil, nil
}

func (f *fakeDashboardTunnelClient) ListTunnelSessionEvents(context.Context, *tunnelv1.ListTunnelSessionEventsRequest, ...grpc.CallOption) (*tunnelv1.ListTunnelSessionEventsResponse, error) {
	return &tunnelv1.ListTunnelSessionEventsResponse{}, nil
}

func (f *fakeDashboardTunnelClient) InspectTunnelSession(context.Context, *tunnelv1.InspectTunnelSessionRequest, ...grpc.CallOption) (*tunnelv1.InspectTunnelSessionResponse, error) {
	return &tunnelv1.InspectTunnelSessionResponse{}, nil
}

type fakeDashboardQuotaClient struct {
	quotas             []*quotav1.NamespaceQuota
	events             []*quotav1.NamespaceQuotaEvent
	lastEventNamespace string
	lastEventLimit     int32
}

func (f *fakeDashboardQuotaClient) GetNamespaceQuota(context.Context, *quotav1.GetNamespaceQuotaRequest, ...grpc.CallOption) (*quotav1.GetNamespaceQuotaResponse, error) {
	return &quotav1.GetNamespaceQuotaResponse{}, nil
}

func (f *fakeDashboardQuotaClient) ListNamespaceQuotas(context.Context, *quotav1.ListNamespaceQuotasRequest, ...grpc.CallOption) (*quotav1.ListNamespaceQuotasResponse, error) {
	return &quotav1.ListNamespaceQuotasResponse{Quotas: f.quotas}, nil
}

func (f *fakeDashboardQuotaClient) ListNamespaceQuotaEvents(_ context.Context, req *quotav1.ListNamespaceQuotaEventsRequest, _ ...grpc.CallOption) (*quotav1.ListNamespaceQuotaEventsResponse, error) {
	f.lastEventNamespace = req.GetNamespace()
	f.lastEventLimit = req.GetLimit()
	return &quotav1.ListNamespaceQuotaEventsResponse{Events: f.events}, nil
}

func (f *fakeDashboardQuotaClient) SetNamespaceQuota(context.Context, *quotav1.SetNamespaceQuotaRequest, ...grpc.CallOption) (*quotav1.SetNamespaceQuotaResponse, error) {
	return &quotav1.SetNamespaceQuotaResponse{}, nil
}

func (f *fakeDashboardQuotaClient) UnsetNamespaceQuota(context.Context, *quotav1.UnsetNamespaceQuotaRequest, ...grpc.CallOption) (*quotav1.UnsetNamespaceQuotaResponse, error) {
	return &quotav1.UnsetNamespaceQuotaResponse{}, nil
}

type fakeDashboardAdminLifecycleClient struct {
	retries     []*adminv1.AllocationLifecycleRetry
	actionRetry *adminv1.AllocationLifecycleRetry
	called      string
}

func (f *fakeDashboardAdminLifecycleClient) ListAllocationLifecycleRetries(context.Context, *adminv1.ListAllocationLifecycleRetriesRequest, ...grpc.CallOption) (*adminv1.ListAllocationLifecycleRetriesResponse, error) {
	return &adminv1.ListAllocationLifecycleRetriesResponse{Retries: f.retries}, nil
}

func (f *fakeDashboardAdminLifecycleClient) ForceAllocationLifecycleRetry(context.Context, *adminv1.ForceAllocationLifecycleRetryRequest, ...grpc.CallOption) (*adminv1.ForceAllocationLifecycleRetryResponse, error) {
	f.called = "force"
	return &adminv1.ForceAllocationLifecycleRetryResponse{Retry: f.actionRetry}, nil
}

func (f *fakeDashboardAdminLifecycleClient) FailAllocationLifecycleRetry(context.Context, *adminv1.FailAllocationLifecycleRetryRequest, ...grpc.CallOption) (*adminv1.FailAllocationLifecycleRetryResponse, error) {
	f.called = "fail"
	return &adminv1.FailAllocationLifecycleRetryResponse{FailedRetry: f.actionRetry}, nil
}

func (f *fakeDashboardAdminLifecycleClient) ClearAllocationLifecycleRetry(context.Context, *adminv1.ClearAllocationLifecycleRetryRequest, ...grpc.CallOption) (*adminv1.ClearAllocationLifecycleRetryResponse, error) {
	f.called = "clear"
	return &adminv1.ClearAllocationLifecycleRetryResponse{ClearedRetry: f.actionRetry}, nil
}

type fakeDashboardAdminAuditClient struct {
	events []*adminv1.AdminAuditEvent
}

func (f *fakeDashboardAdminAuditClient) ListAdminAuditEvents(context.Context, *adminv1.ListAdminAuditEventsRequest, ...grpc.CallOption) (*adminv1.ListAdminAuditEventsResponse, error) {
	return &adminv1.ListAdminAuditEventsResponse{Events: f.events}, nil
}

type fakeDashboardReliabilityClient struct {
	health *adminv1.AdminReliabilityHealth
}

func (f *fakeDashboardReliabilityClient) CheckConsistency(context.Context, *adminv1.CheckConsistencyRequest, ...grpc.CallOption) (*adminv1.CheckConsistencyResponse, error) {
	if f.health == nil {
		return &adminv1.CheckConsistencyResponse{}, nil
	}
	return &adminv1.CheckConsistencyResponse{Snapshot: f.health.GetConsistency()}, nil
}

func (f *fakeDashboardReliabilityClient) GetAdminReliabilityHealth(context.Context, *adminv1.GetAdminReliabilityHealthRequest, ...grpc.CallOption) (*adminv1.GetAdminReliabilityHealthResponse, error) {
	return &adminv1.GetAdminReliabilityHealthResponse{Health: f.health}, nil
}

func TestIsLoopbackListen(t *testing.T) {
	if !isLoopbackListen("127.0.0.1:0") {
		t.Fatal("127.0.0.1:0 should be loopback")
	}
	if isLoopbackListen("0.0.0.0:0") {
		t.Fatal("0.0.0.0:0 should not be accepted")
	}
}
