package agent

import (
	"context"
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func TestListRuntimesQueriesAllNamespacesAndDerivesLifecycle(t *testing.T) {
	client := &fakeServiceClient{listServicesResp: &servicev1.ListServicesResponse{
		Services: []*servicev1.Service{{
			ID: "svc-b", Namespace: "default", Status: servicev1.ServiceStatus_SERVICE_STATUS_READY,
			Replicas: 1, ReadyReplicas: 1, EnvironmentID: "env-b",
			Config: workspaceExecutionConfig("project-b", testBundleRuntime()),
			Labels: map[string]string{LabelWorkflow: "agent", LabelWorkspace: "project-b", LabelAgent: "codex", LabelProfile: "b"},
		}, {
			ID: "svc-a", Namespace: "agents", Status: servicev1.ServiceStatus_SERVICE_STATUS_READY,
			Replicas: 0, EnvironmentID: "env-a",
			Config: workspaceExecutionConfig("project-a", testBundleRuntime()),
			Labels: map[string]string{LabelWorkflow: "agent", LabelWorkspace: "project-a", LabelAgent: "claude-code", LabelProfile: "a"},
		}, {
			ID: "svc-unrelated", Namespace: "agents", Status: servicev1.ServiceStatus_SERVICE_STATUS_READY,
			Replicas: 1, Labels: map[string]string{LabelWorkflow: "service"},
		}},
	}}

	runtimes, err := (Control{}).ListRuntimes(context.Background(), client, "", "")
	if err != nil {
		t.Fatalf("ListRuntimes() error = %v", err)
	}
	if len(runtimes) != 2 || runtimes[0].Workspace != "project-a" || runtimes[0].LifecycleState != LifecycleSuspended ||
		runtimes[1].Workspace != "project-b" || runtimes[1].LifecycleState != LifecycleRunning || !runtimes[1].Persistent {
		t.Fatalf("ListRuntimes() = %+v", runtimes)
	}
	if client.listServicesCalls != 1 || len(client.listServicesReq.GetFilter().GetStatuses()) == 0 || client.listServicesReq.GetFilter().GetNamespace() != "" {
		t.Fatalf("global ListServices request = %#v calls=%d", client.listServicesReq, client.listServicesCalls)
	}
}

func TestListActiveAgentServicesPaginatesScopedQuery(t *testing.T) {
	client := &fakeServiceClient{listServicesResponses: []*servicev1.ListServicesResponse{
		{Services: []*servicev1.Service{{ID: "svc-a"}}, NextCursor: "page-2"},
		{Services: []*servicev1.Service{{ID: "svc-b"}}},
	}}
	services, err := listActiveAgentServices(context.Background(), client, "agents", map[string]string{LabelWorkflow: "agent"})
	if err != nil || len(services) != 2 || client.listServicesCalls != 2 {
		t.Fatalf("listActiveAgentServices() = %#v err=%v calls=%d", services, err, client.listServicesCalls)
	}
	if client.listServicesReq.GetFilter().GetNamespace() != "agents" || client.listServicesReq.GetFilter().GetCursor() != "page-2" {
		t.Fatalf("last scoped request = %#v", client.listServicesReq)
	}
}

func TestStopScalesWorkspaceToZero(t *testing.T) {
	service := &servicev1.Service{ID: "svc-a", Namespace: "agents", Version: 7, Replicas: 1,
		Status: servicev1.ServiceStatus_SERVICE_STATUS_READY,
		Labels: map[string]string{LabelWorkflow: "agent", LabelWorkspace: "project-a"}}
	client := &fakeServiceClient{
		listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{service}},
		getResp:          &servicev1.GetServiceResponse{Service: service},
		updateResp:       &servicev1.UpdateServiceResponse{Service: &servicev1.Service{ID: "svc-a", Replicas: 0}},
	}

	result, err := (Control{}).Stop(context.Background(), client, "project-a")
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if result.Workspace != "project-a" || result.ServiceID != "svc-a" || result.LifecycleState != LifecycleSuspended {
		t.Fatalf("Stop() = %+v", result)
	}
	if len(client.updateReqs) != 1 || client.updateReqs[0].GetReplicas() != 0 || client.updateReqs[0].GetExpectedVersion() != 7 {
		t.Fatalf("update requests = %#v", client.updateReqs)
	}
	if client.listServicesReq.GetFilter().GetNamespace() != "" || len(client.listServicesReq.GetFilter().GetStatuses()) == 0 {
		t.Fatalf("Stop() must locate globally unique workspace without namespace filter: %#v", client.listServicesReq)
	}
}

func TestStopIsIdempotentForSuspendedWorkspace(t *testing.T) {
	service := &servicev1.Service{ID: "svc-a", Version: 2, Replicas: 0,
		Status: servicev1.ServiceStatus_SERVICE_STATUS_READY,
		Config: &commonv1.ExecutionConfig{}, Labels: map[string]string{LabelWorkflow: "agent", LabelWorkspace: "project-a"}}
	client := &fakeServiceClient{
		listServicesResp: &servicev1.ListServicesResponse{Services: []*servicev1.Service{service}},
		getResp:          &servicev1.GetServiceResponse{Service: service},
	}
	result, err := (Control{}).Stop(context.Background(), client, "project-a")
	if err != nil || result.ServiceID != "svc-a" || len(client.updateReqs) != 0 {
		t.Fatalf("Stop() = %+v err=%v updates=%d", result, err, len(client.updateReqs))
	}
}

func TestStopRejectsUnknownWorkspace(t *testing.T) {
	client := &fakeServiceClient{listServicesResp: &servicev1.ListServicesResponse{}}
	if _, err := (Control{}).Stop(context.Background(), client, "missing"); err == nil {
		t.Fatal("Stop() error = nil, want workspace not found")
	}
}

func TestWorkspaceConfigRequiresCanonicalMountOptions(t *testing.T) {
	service := &servicev1.Service{
		Config: workspaceExecutionConfig("project-a", testBundleRuntime()),
		Labels: map[string]string{LabelWorkspace: "project-a"},
	}
	service.Config.Resources = &commonv1.ResourceSpec{Requests: &commonv1.ResourceQuantity{CpuMilli: 500, MemoryBytes: 4 << 30}}
	if !workspaceConfigMatches(service, testBundleRuntime()) {
		t.Fatal("canonical workspace mount with server-defaulted resources was not recognized")
	}
	service.Config.VolumeMounts[0].Options = []string{"rw"}
	if workspaceConfigMatches(service, testBundleRuntime()) {
		t.Fatal("workspace config without nosuid,nodev was recognized")
	}
}

func TestWorkspaceLifecycleState(t *testing.T) {
	tests := []struct {
		service *servicev1.Service
		want    string
	}{
		{&servicev1.Service{Replicas: 0}, LifecycleSuspended},
		{&servicev1.Service{Replicas: 1, ReadyReplicas: 1}, LifecycleRunning},
		{&servicev1.Service{Replicas: 1}, LifecycleStarting},
		{&servicev1.Service{Replicas: 1, UnhealthyReplicas: 1}, LifecycleDegraded},
	}
	for _, test := range tests {
		if got := workspaceLifecycleState(test.service); got != test.want {
			t.Fatalf("workspaceLifecycleState(%+v) = %q, want %q", test.service, got, test.want)
		}
	}
}
