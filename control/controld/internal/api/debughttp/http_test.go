package debughttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	consistencykernel "github.com/cofy-x/axern/control/controld/internal/kernel/consistency"
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	reconcilekernel "github.com/cofy-x/axern/control/controld/internal/kernel/reconcile"
	catalogv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/catalog/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
)

func TestNodesHandlerReturnsJSONDebugShape(t *testing.T) {
	handler := New(Config{
		DebugNodes: func() []nodekernel.DebugNode {
			return []nodekernel.DebugNode{{
				NodeID:           "node-a",
				FreshnessState:   "fresh",
				HeartbeatAgeSecs: 1,
				SummaryAgeSecs:   2,
				RegisteredAt:     time.Date(2026, 4, 21, 10, 0, 0, 0, time.UTC),
			}}
		},
		ResourcePolicy: func() ResourcePolicySnapshot {
			return ResourcePolicySnapshot{CPUOvercommitRatio: 2, MemoryOvercommitPolicy: "disabled"}
		},
		ListRuntimeTemplates: func(context.Context) (*catalogv1.ListRuntimeTemplatesResponse, error) {
			return &catalogv1.ListRuntimeTemplatesResponse{}, nil
		},
		ListNamespaceQuotas: func(context.Context) (*quotav1.ListNamespaceQuotasResponse, error) {
			return &quotav1.ListNamespaceQuotasResponse{}, nil
		},
		ListReconcileQueue: func(context.Context) ([]allocationkernel.LifecycleRetryItem, error) {
			return nil, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/nodesz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), "\"node_id\":\"node-a\"") {
		t.Fatalf("unexpected nodes body: %s", recorder.Body.String())
	}
}

func TestResourceHandlerReturnsJSONPolicyShape(t *testing.T) {
	handler := New(Config{
		DebugNodes: func() []nodekernel.DebugNode { return nil },
		ResourcePolicy: func() ResourcePolicySnapshot {
			return ResourcePolicySnapshot{CPUOvercommitRatio: 2.5, MemoryOvercommitPolicy: "disabled"}
		},
		ListRuntimeTemplates: func(context.Context) (*catalogv1.ListRuntimeTemplatesResponse, error) {
			return &catalogv1.ListRuntimeTemplatesResponse{}, nil
		},
		ListNamespaceQuotas: func(context.Context) (*quotav1.ListNamespaceQuotasResponse, error) {
			return &quotav1.ListNamespaceQuotasResponse{}, nil
		},
		ListReconcileQueue: func(context.Context) ([]allocationkernel.LifecycleRetryItem, error) {
			return nil, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/resourcez", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	for _, want := range []string{`"cpu_overcommit_ratio":2.5`, `"memory_overcommit_policy":"disabled"`} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("unexpected resource body: %s", recorder.Body.String())
		}
	}
}

func TestQuotaHandlerReturnsProtoJSON(t *testing.T) {
	handler := New(Config{
		DebugNodes: func() []nodekernel.DebugNode { return nil },
		ResourcePolicy: func() ResourcePolicySnapshot {
			return ResourcePolicySnapshot{}
		},
		ListRuntimeTemplates: func(context.Context) (*catalogv1.ListRuntimeTemplatesResponse, error) {
			return &catalogv1.ListRuntimeTemplatesResponse{}, nil
		},
		ListNamespaceQuotas: func(context.Context) (*quotav1.ListNamespaceQuotasResponse, error) {
			return &quotav1.ListNamespaceQuotasResponse{Quotas: []*quotav1.NamespaceQuota{{Namespace: "default"}}}, nil
		},
		ListReconcileQueue: func(context.Context) ([]allocationkernel.LifecycleRetryItem, error) {
			return nil, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/quotasz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"namespace":"default"`) {
		t.Fatalf("unexpected quotas body: %s", recorder.Body.String())
	}
}

func TestAllocationReconcileHandlerReturnsJSONQueue(t *testing.T) {
	handler := New(Config{
		DebugNodes:     func() []nodekernel.DebugNode { return nil },
		ResourcePolicy: func() ResourcePolicySnapshot { return ResourcePolicySnapshot{} },
		ListRuntimeTemplates: func(context.Context) (*catalogv1.ListRuntimeTemplatesResponse, error) {
			return &catalogv1.ListRuntimeTemplatesResponse{}, nil
		},
		ListNamespaceQuotas: func(context.Context) (*quotav1.ListNamespaceQuotasResponse, error) {
			return &quotav1.ListNamespaceQuotasResponse{}, nil
		},
		ListReconcileQueue: func(context.Context) ([]allocationkernel.LifecycleRetryItem, error) {
			return []allocationkernel.LifecycleRetryItem{{
				AllocationID:      "alloc-1",
				OwnerID:           "svc-1",
				OwnerType:         allocationkernel.OwnerService,
				Reason:            allocationkernel.ReconcileReasonCreate,
				NodeID:            "node-a",
				ReconcileAttempts: 2,
				AgeSeconds:        30,
				Due:               true,
			}}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/allocation-reconcilez", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	for _, want := range []string{`"allocation_id":"alloc-1"`, `"owner_type":"service"`, `"reason":"create"`, `"reconcile_attempts":2`, `"due":true`} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("unexpected allocation reconcile body: %s", recorder.Body.String())
		}
	}
}

func TestReconcileHealthHandlerReturnsJSONSnapshot(t *testing.T) {
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	handler := New(Config{
		DebugNodes:     func() []nodekernel.DebugNode { return nil },
		ResourcePolicy: func() ResourcePolicySnapshot { return ResourcePolicySnapshot{} },
		ListRuntimeTemplates: func(context.Context) (*catalogv1.ListRuntimeTemplatesResponse, error) {
			return &catalogv1.ListRuntimeTemplatesResponse{}, nil
		},
		ListNamespaceQuotas: func(context.Context) (*quotav1.ListNamespaceQuotasResponse, error) {
			return &quotav1.ListNamespaceQuotasResponse{}, nil
		},
		ListReconcileQueue: func(context.Context) ([]allocationkernel.LifecycleRetryItem, error) {
			return nil, nil
		},
		ReconcileHealth: func() reconcilekernel.HealthSnapshot {
			return reconcilekernel.HealthSnapshot{Components: []reconcilekernel.ComponentHealth{{
				Component:           reconcilekernel.ComponentService,
				LastErrorAt:         &now,
				LastError:           "database unavailable",
				ConsecutiveFailures: 2,
			}}}
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/reconcilez", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	for _, want := range []string{`"component":"service"`, `"last_error":"database unavailable"`, `"consecutive_failures":2`} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("unexpected reconcile health body: %s", recorder.Body.String())
		}
	}
}

func TestReconcileHealthHandlerReturnsStableEmptySnapshot(t *testing.T) {
	handler := New(Config{})

	req := httptest.NewRequest(http.MethodGet, "/reconcilez", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	if !strings.Contains(recorder.Body.String(), `"components":[]`) {
		t.Fatalf("unexpected reconcile health body: %s", recorder.Body.String())
	}
}

func TestConsistencyHandlerReturnsJSONSnapshot(t *testing.T) {
	handler := New(Config{
		ConsistencySnapshot: func(context.Context) (consistencykernel.Snapshot, error) {
			return consistencykernel.NewSnapshot(consistencykernel.Counts{ActiveReservations: 1}, []consistencykernel.Issue{{
				Code:         "active_reservation_on_ended_allocation",
				Severity:     consistencykernel.SeverityError,
				AllocationID: "alloc-1",
				OwnerType:    allocationkernel.OwnerRun,
				OwnerID:      "run-1",
			}}, false), nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/consistencyz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	for _, want := range []string{`"status":"inconsistent"`, `"active_reservations":1`, `"issues":1`, `"code":"active_reservation_on_ended_allocation"`, `"allocation_id":"alloc-1"`} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("unexpected consistency body: %s", recorder.Body.String())
		}
	}
}

func TestConsistencyHandlerReturnsStableEmptySnapshot(t *testing.T) {
	handler := New(Config{
		ConsistencySnapshot: func(context.Context) (consistencykernel.Snapshot, error) {
			return consistencykernel.NewSnapshot(consistencykernel.Counts{}, nil, false), nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/consistencyz", nil)
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	for _, want := range []string{`"status":"ok"`, `"issues":[]`} {
		if !strings.Contains(recorder.Body.String(), want) {
			t.Fatalf("unexpected consistency body: %s", recorder.Body.String())
		}
	}
}
