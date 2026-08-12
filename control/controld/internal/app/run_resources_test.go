package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	"github.com/cofy-x/axern/control/controld/internal/testutil/controldtest"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	runv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/run/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestPostgresRunResourceNormalizationPlacementAndReservations(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 5, 8, 16, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	env := createDefaultEnvironment(t, app)

	small := controldtest.ReadySummary(now)
	small.Allocatable.CpuMilli = 400
	small.Capacity.CpuMilli = 400
	controldtest.SetReadySummaryMemory(small, 1<<30)
	reportReadyNodeSummary(t, app, "node-a", now, small)

	_, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/sleep", "60"}},
	})
	if grpcstatus.Code(err) != codes.FailedPrecondition {
		t.Fatalf("CreateRun(omitted resources on small node) code = %v, want FailedPrecondition", grpcstatus.Code(err))
	}
	for _, want := range []string{
		fmt.Sprintf("requested cpu_milli=%d memory_bytes=%d", executionkernel.DefaultCPUMilli, executionkernel.DefaultMemoryBytes),
		"insufficient_cpu",
		"insufficient_memory",
	} {
		if !strings.Contains(grpcstatus.Convert(err).Message(), want) {
			t.Fatalf("CreateRun error = %q, want to contain %q", grpcstatus.Convert(err).Message(), want)
		}
	}

	large := controldtest.ReadySummary(now.Add(time.Second))
	reportReadyNodeSummary(t, app, "node-a", now.Add(time.Second), large)
	defaulted, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config:        &commonv1.ExecutionConfig{Argv: []string{"/bin/sleep", "60"}},
	})
	if err != nil {
		t.Fatalf("CreateRun(defaulted resources) error = %v", err)
	}
	defaultRequests := defaulted.GetRun().GetConfig().GetResources().GetRequests()
	if defaultRequests.GetCpuMilli() != executionkernel.DefaultCPUMilli {
		t.Fatalf("default cpu request = %d, want %d", defaultRequests.GetCpuMilli(), executionkernel.DefaultCPUMilli)
	}
	if defaultRequests.GetMemoryBytes() != executionkernel.DefaultMemoryBytes {
		t.Fatalf("default memory request = %d, want %d", defaultRequests.GetMemoryBytes(), executionkernel.DefaultMemoryBytes)
	}
	defaultLimits := defaulted.GetRun().GetConfig().GetResources().GetLimits()
	if defaultLimits == nil || defaultLimits.GetCpuMilli() != 0 || defaultLimits.GetMemoryBytes() != 0 ||
		defaultLimits.GetEphemeralStorageBytes() != executionkernel.DefaultEphemeralStorageBytes {
		t.Fatalf("default limits = %#v, want only ephemeral_storage_bytes=%d", defaultLimits, executionkernel.DefaultEphemeralStorageBytes)
	}
	assertActiveReservation(t, app, defaulted.GetRun().GetAllocationID(), executionkernel.DefaultCPUMilli, executionkernel.DefaultMemoryBytes)

	requestOnly, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
				CpuMilli:    250,
				MemoryBytes: 128 << 20,
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateRun(request-only resources) error = %v", err)
	}
	requestOnlyResources := requestOnly.GetRun().GetConfig().GetResources()
	if requestOnlyResources.GetRequests().GetCpuMilli() != 250 || requestOnlyResources.GetRequests().GetMemoryBytes() != 128<<20 {
		t.Fatalf("request-only requests = %+v, want 250/128MiB", requestOnlyResources.GetRequests())
	}
	requestOnlyLimits := requestOnlyResources.GetLimits()
	if requestOnlyLimits == nil || requestOnlyLimits.GetCpuMilli() != 0 || requestOnlyLimits.GetMemoryBytes() != 0 ||
		requestOnlyLimits.GetEphemeralStorageBytes() != executionkernel.DefaultEphemeralStorageBytes {
		t.Fatalf("request-only limits = %#v, want only ephemeral_storage_bytes=%d", requestOnlyLimits, executionkernel.DefaultEphemeralStorageBytes)
	}
	assertActiveReservation(t, app, requestOnly.GetRun().GetAllocationID(), 250, 128<<20)

	_, err = public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{
				Requests: &commonv1.ResourceQuantity{CpuMilli: 1000},
				Limits:   &commonv1.ResourceQuantity{CpuMilli: 500},
			},
		},
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("CreateRun(limit below request) code = %v, want InvalidArgument", grpcstatus.Code(err))
	}
	if got, want := grpcstatus.Convert(err).Message(), "request=1000 limit=500"; !strings.Contains(got, want) {
		t.Fatalf("CreateRun(limit below request) message = %q, want to contain %q", got, want)
	}
}

func TestPostgresRunAdmissionBalancesStalePlacementSnapshots(t *testing.T) {
	app, _ := newPostgresTestService(t)
	defer app.Close()
	now := time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	env := createDefaultEnvironment(t, app)

	for _, nodeID := range []string{"node-a", "node-b"} {
		summary := controldtest.ReadySummary(now)
		summary.Allocatable.CpuMilli = 2000
		summary.Capacity.CpuMilli = 2000
		controldtest.SetReadySummaryMemory(summary, 4<<30)
		reportReadyNodeSummary(t, app, nodeID, now, summary)
	}
	for range 4 {
		if _, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
			EnvironmentID: env.GetID(),
			Config: &commonv1.ExecutionConfig{
				Argv: []string{"/bin/sleep", "60"},
				Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
					CpuMilli:    100,
					MemoryBytes: 128 << 20,
				}},
			},
		}); err != nil {
			t.Fatalf("CreateRun() error = %v", err)
		}
	}

	rows, err := app.db.Pool().Query(context.Background(), `
		SELECT node_id, count(*)
		FROM workload_reservations
		WHERE released_at IS NULL
		GROUP BY node_id
	`)
	if err != nil {
		t.Fatalf("query reservations by node: %v", err)
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var nodeID string
		var count int
		if err := rows.Scan(&nodeID, &count); err != nil {
			t.Fatalf("scan reservation count: %v", err)
		}
		counts[nodeID] = count
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate reservation counts: %v", err)
	}
	if counts["node-a"] != 2 || counts["node-b"] != 2 {
		t.Fatalf("reservation distribution = %#v, want 2 allocations per node", counts)
	}
}

func TestPostgresResourceCPUOvercommitAdmission(t *testing.T) {
	app, _ := newPostgresTestServiceWithConfig(t, Config{
		ResourcePolicy: resourcekernel.AdmissionPolicy{CPUOvercommitRatio: 2},
	})
	defer app.Close()
	now := time.Date(2026, 5, 8, 17, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	env := createDefaultEnvironment(t, app)

	summary := controldtest.ReadySummary(now)
	summary.Allocatable.CpuMilli = 1000
	summary.Capacity.CpuMilli = 1000
	controldtest.SetReadySummaryMemory(summary, 8<<30)
	reportReadyNodeSummary(t, app, "node-a", now, summary)

	first, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
				CpuMilli:    900,
				MemoryBytes: 128 << 20,
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateRun(first overcommit) error = %v", err)
	}
	assertActiveReservation(t, app, first.GetRun().GetAllocationID(), 900, 128<<20)

	second, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
				CpuMilli:    900,
				MemoryBytes: 128 << 20,
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateRun(second overcommit) error = %v", err)
	}
	assertActiveReservation(t, app, second.GetRun().GetAllocationID(), 900, 128<<20)

	_, err = public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
				CpuMilli:    300,
				MemoryBytes: 128 << 20,
			}},
		},
	})
	if grpcstatus.Code(err) != codes.ResourceExhausted {
		t.Fatalf("CreateRun(over effective cpu) code = %v, want ResourceExhausted", grpcstatus.Code(err))
	}
	for _, want := range []string{
		"node_id=node-a",
		"cpu requested_milli=300 reserved_milli=1800 effective_allocatable_milli=2000 available_milli=200 overcommit_ratio=2",
	} {
		if !strings.Contains(grpcstatus.Convert(err).Message(), want) {
			t.Fatalf("CreateRun(over effective cpu) message = %q, want to contain %q", grpcstatus.Convert(err).Message(), want)
		}
	}
}

func TestPostgresResourceMemoryDoesNotOvercommit(t *testing.T) {
	app, _ := newPostgresTestServiceWithConfig(t, Config{
		ResourcePolicy: resourcekernel.AdmissionPolicy{CPUOvercommitRatio: 2},
	})
	defer app.Close()
	now := time.Date(2026, 5, 8, 18, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	env := createDefaultEnvironment(t, app)

	summary := controldtest.ReadySummary(now)
	summary.Allocatable.CpuMilli = 4000
	summary.Capacity.CpuMilli = 4000
	controldtest.SetReadySummaryMemory(summary, 512<<20)
	reportReadyNodeSummary(t, app, "node-a", now, summary)

	_, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
				CpuMilli:    500,
				MemoryBytes: 400 << 20,
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateRun(first memory reservation) error = %v", err)
	}

	_, err = public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
				CpuMilli:    500,
				MemoryBytes: 200 << 20,
			}},
		},
	})
	if grpcstatus.Code(err) != codes.ResourceExhausted {
		t.Fatalf("CreateRun(over memory) code = %v, want ResourceExhausted", grpcstatus.Code(err))
	}
	for _, want := range []string{
		"node_id=node-a",
		"memory requested_bytes=209715200 reserved_bytes=419430400 effective_allocatable_bytes=536870912 available_bytes=117440512",
	} {
		if !strings.Contains(grpcstatus.Convert(err).Message(), want) {
			t.Fatalf("CreateRun(over memory) message = %q, want to contain %q", grpcstatus.Convert(err).Message(), want)
		}
	}
}

func TestPostgresRunNamespaceResourceQuotaAdmission(t *testing.T) {
	app, _ := newPostgresTestServiceWithConfig(t, Config{
		ResourcePolicy: resourcekernel.AdmissionPolicy{CPUOvercommitRatio: 2},
	})
	defer app.Close()
	now := time.Date(2026, 5, 8, 18, 30, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	env := createDefaultEnvironment(t, app)
	setNamespaceQuota(t, app, "default", int64(1000), int64(1<<30))

	summary := controldtest.ReadySummary(now)
	summary.Allocatable.CpuMilli = 1000
	summary.Capacity.CpuMilli = 1000
	controldtest.SetReadySummaryMemory(summary, 8<<30)
	reportReadyNodeSummary(t, app, "node-a", now, summary)

	first, err := public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
				CpuMilli:    800,
				MemoryBytes: 256 << 20,
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateRun(first quota admission) error = %v", err)
	}
	assertActiveReservation(t, app, first.GetRun().GetAllocationID(), 800, 256<<20)

	_, err = public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
				CpuMilli:    300,
				MemoryBytes: 256 << 20,
			}},
		},
	})
	if grpcstatus.Code(err) != codes.ResourceExhausted {
		t.Fatalf("CreateRun(over namespace cpu quota) code = %v, want ResourceExhausted", grpcstatus.Code(err))
	}
	for _, want := range []string{
		"namespace quota exceeded: namespace=default",
		"cpu requested_milli=300 reserved_milli=800 limit_milli=1000 available_milli=200",
	} {
		if !strings.Contains(grpcstatus.Convert(err).Message(), want) {
			t.Fatalf("CreateRun(over namespace cpu quota) message = %q, want to contain %q", grpcstatus.Convert(err).Message(), want)
		}
	}

	_, err = public.CreateRun(context.Background(), &runv1.CreateRunRequest{
		EnvironmentID: env.GetID(),
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
				CpuMilli:    100,
				MemoryBytes: 800 << 20,
			}},
		},
	})
	if grpcstatus.Code(err) != codes.ResourceExhausted {
		t.Fatalf("CreateRun(over namespace memory quota) code = %v, want ResourceExhausted", grpcstatus.Code(err))
	}
	for _, want := range []string{
		"namespace quota exceeded: namespace=default",
		"memory requested_bytes=838860800 reserved_bytes=268435456 limit_bytes=1073741824 available_bytes=805306368",
	} {
		if !strings.Contains(grpcstatus.Convert(err).Message(), want) {
			t.Fatalf("CreateRun(over namespace memory quota) message = %q, want to contain %q", grpcstatus.Convert(err).Message(), want)
		}
	}
	eventsResp, err := public.ListNamespaceQuotaEvents(context.Background(), &quotav1.ListNamespaceQuotaEventsRequest{Namespace: "default"})
	if err != nil {
		t.Fatalf("ListNamespaceQuotaEvents() error = %v", err)
	}
	events := eventsResp.GetEvents()
	if len(events) != 2 {
		t.Fatalf("quota event count = %d, want 2", len(events))
	}
	reasons := map[quotav1.NamespaceQuotaEventReason]bool{}
	for _, event := range events {
		reasons[event.GetReason()] = true
	}
	for _, want := range []quotav1.NamespaceQuotaEventReason{
		quotav1.NamespaceQuotaEventReason_NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_CPU,
		quotav1.NamespaceQuotaEventReason_NAMESPACE_QUOTA_EVENT_REASON_INSUFFICIENT_MEMORY,
	} {
		if !reasons[want] {
			t.Fatalf("quota event reasons = %v, missing %v", reasons, want)
		}
	}
	for _, event := range events {
		if event.GetWorkloadType() != quotav1.NamespaceQuotaEventWorkloadType_NAMESPACE_QUOTA_EVENT_WORKLOAD_TYPE_RUN || event.GetWorkloadID() == "" {
			t.Fatalf("run quota event workload = type:%v id:%q, want run with generated id", event.GetWorkloadType(), event.GetWorkloadID())
		}
	}
}

func TestPostgresServiceResourceCPUOvercommitAdmission(t *testing.T) {
	app, _ := newPostgresTestServiceWithConfig(t, Config{
		ResourcePolicy: resourcekernel.AdmissionPolicy{CPUOvercommitRatio: 2},
	})
	defer app.Close()
	now := time.Date(2026, 5, 8, 19, 0, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	env := createDefaultEnvironment(t, app)

	summary := controldtest.ReadySummary(now)
	summary.Allocatable.CpuMilli = 1000
	summary.Capacity.CpuMilli = 1000
	controldtest.SetReadySummaryMemory(summary, 8<<30)
	reportReadyNodeSummary(t, app, "node-a", now, summary)

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      2,
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
				CpuMilli:    900,
				MemoryBytes: 128 << 20,
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateService(overcommit replicas) error = %v", err)
	}
	service := reconcileServiceForTest(t, app, createResp.GetService().GetID(), now)
	if got := len(service.GetAllocationIds()); got != 2 {
		t.Fatalf("service allocation_ids = %d, want 2; service=%+v", got, service)
	}
	for _, allocationID := range service.GetAllocationIds() {
		assertActiveReservation(t, app, allocationID, 900, 128<<20)
	}

	blockedResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
				CpuMilli:    300,
				MemoryBytes: 128 << 20,
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateService(over effective cpu) error = %v", err)
	}
	blocked := reconcileServiceForTest(t, app, blockedResp.GetService().GetID(), now)
	for _, want := range []string{
		"node_id=node-a",
		"cpu requested_milli=300 reserved_milli=1800 effective_allocatable_milli=2000 available_milli=200 overcommit_ratio=2",
	} {
		if !strings.Contains(blocked.GetMessage(), want) {
			t.Fatalf("CreateService(over effective cpu) message = %q, want to contain %q", blocked.GetMessage(), want)
		}
	}
}

func TestPostgresServiceNamespaceResourceQuotaAdmission(t *testing.T) {
	app, _ := newPostgresTestServiceWithConfig(t, Config{
		ResourcePolicy: resourcekernel.AdmissionPolicy{CPUOvercommitRatio: 2},
	})
	defer app.Close()
	now := time.Date(2026, 5, 8, 19, 30, 0, 0, time.UTC)
	app.now = func() time.Time { return now }
	public := app.PublicV1Handler()
	env := createDefaultEnvironment(t, app)
	setNamespaceQuota(t, app, "default", int64(1000), nil)

	summary := controldtest.ReadySummary(now)
	summary.Allocatable.CpuMilli = 1000
	summary.Capacity.CpuMilli = 1000
	controldtest.SetReadySummaryMemory(summary, 8<<30)
	reportReadyNodeSummary(t, app, "node-a", now, summary)

	createResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      2,
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
				CpuMilli:    400,
				MemoryBytes: 128 << 20,
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateService(first quota admission) error = %v", err)
	}
	service := reconcileServiceForTest(t, app, createResp.GetService().GetID(), now)
	if got := len(service.GetAllocationIds()); got != 2 {
		t.Fatalf("service allocation_ids = %d, want 2", got)
	}

	blockedResp, err := public.CreateService(context.Background(), &servicev1.CreateServiceRequest{
		Namespace:     "default",
		EnvironmentID: env.GetID(),
		Replicas:      1,
		Config: &commonv1.ExecutionConfig{
			Argv: []string{"/bin/sleep", "60"},
			Resources: &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{
				CpuMilli:    300,
				MemoryBytes: 128 << 20,
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateService(over namespace cpu quota) error = %v", err)
	}
	blocked := reconcileServiceForTest(t, app, blockedResp.GetService().GetID(), now)
	for _, want := range []string{
		"namespace quota exceeded: namespace=default",
		"cpu requested_milli=300 reserved_milli=800 limit_milli=1000 available_milli=200",
	} {
		if !strings.Contains(blocked.GetMessage(), want) {
			t.Fatalf("CreateService(over namespace cpu quota) message = %q, want to contain %q", blocked.GetMessage(), want)
		}
	}
}

func TestAppRejectsInvalidResourcePolicy(t *testing.T) {
	_, err := New(Config{
		ResourcePolicy: resourcekernel.AdmissionPolicy{CPUOvercommitRatio: -1},
	})
	if err == nil {
		t.Fatal("New(invalid resource policy) returned nil error")
	}
	if !strings.Contains(err.Error(), "resource cpu overcommit ratio must be > 0") {
		t.Fatalf("New(invalid resource policy) error = %q", err)
	}
}

func reportReadyNodeSummary(t *testing.T, app *App, nodeID string, _ time.Time, summary *nodev1.NodeSummary) {
	t.Helper()
	node := app.NodeV1Handler()
	if _, err := node.RegisterNode(context.Background(), &nodev1.RegisterNodeRequest{
		NodeID:        nodeID,
		Runtimes:      []string{"runsc"},
		NodeTarget:    "127.0.0.1:25000",
		NodeAuthToken: "test-node-token",
	}); err != nil {
		t.Fatalf("RegisterNode() error = %v", err)
	}
	if _, err := node.ReportNode(context.Background(), &nodev1.ReportNodeRequest{
		NodeID:        nodeID,
		Runtimes:      []string{"runsc"},
		NodeTarget:    "127.0.0.1:25000",
		NodeAuthToken: "test-node-token",
		Summary:       summary,
	}); err != nil {
		t.Fatalf("ReportNode() error = %v", err)
	}
}

func reconcileServiceForTest(t *testing.T, app *App, serviceID string, now time.Time) *servicev1.Service {
	t.Helper()
	if err := app.serviceReconciler.ReconcilePending(context.Background(), now); err != nil {
		t.Fatalf("ReconcilePending() error = %v", err)
	}
	resp, err := app.PublicV1Handler().GetService(context.Background(), &servicev1.GetServiceRequest{ServiceID: serviceID})
	if err != nil {
		t.Fatalf("GetService() error = %v", err)
	}
	return resp.GetService()
}

func assertActiveReservation(t *testing.T, app *App, allocationID string, wantCPU, wantMemory int64) {
	t.Helper()
	var cpu, memory int64
	if err := app.db.Pool().QueryRow(context.Background(), `
		SELECT cpu_milli, sandbox_memory_request_bytes
		FROM workload_reservations
		WHERE allocation_id = $1 AND released_at IS NULL
	`, allocationID).Scan(&cpu, &memory); err != nil {
		t.Fatalf("query active reservation for %s: %v", allocationID, err)
	}
	if cpu != wantCPU || memory != wantMemory {
		t.Fatalf("reservation for %s = cpu %d memory %d, want cpu %d memory %d", allocationID, cpu, memory, wantCPU, wantMemory)
	}
}

func setNamespaceQuota(t *testing.T, app *App, namespace string, cpuMilliLimit any, memoryBytesLimit any) {
	t.Helper()
	if _, err := app.db.Pool().Exec(context.Background(), `
		UPDATE namespace_resource_quotas
		SET cpu_milli_limit = $2, memory_bytes_limit = $3, version = version + 1, updated_at = now()
		WHERE namespace = $1
	`, namespace, cpuMilliLimit, memoryBytesLimit); err != nil {
		t.Fatalf("set namespace quota: %v", err)
	}
}
