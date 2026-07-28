package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	demonginx "github.com/cofy-x/axern/runtime/axnoded/internal/demo/nginx"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	axmetrics "github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/proto"
)

type fakeSandboxService struct {
	ready              bool
	inventory          nodeinventory.NodeInventorySnapshot
	inventoryReady     bool
	runtimeStatuses    []service.RuntimeStatus
	startRequests      []*runtimeapi.StartRequest
	deleteRequests     []*runtimeapi.DeleteRequest
	listRequests       []*runtimeapi.ListContainersRequest
	containers         []*runtimeapi.ContainerStatus
	controlPlaneHealth service.ControlPlaneReporterHealth
}

func (f *fakeSandboxService) ControlPlaneReporterHealth() service.ControlPlaneReporterHealth {
	return f.controlPlaneHealth
}

func (f *fakeSandboxService) Run(context.Context) error      { return nil }
func (f *fakeSandboxService) Shutdown(context.Context) error { return nil }
func (f *fakeSandboxService) Start(_ context.Context, req *runtimeapi.StartRequest) (*runtimeapi.StartResponse, error) {
	f.startRequests = append(f.startRequests, proto.Clone(req).(*runtimeapi.StartRequest))
	f.containers = append(f.containers, &runtimeapi.ContainerStatus{
		ID:      req.GetContainerID(),
		Runtime: req.GetRuntimeTemplate().GetSandbox(),
		State:   runtimeapi.ContainerState_CONTAINER_RUNNING,
	})
	return &runtimeapi.StartResponse{Code: 0, ID: req.GetContainerID(), Message: "ok"}, nil
}
func (f *fakeSandboxService) Delete(_ context.Context, req *runtimeapi.DeleteRequest) (*runtimeapi.DeleteResponse, error) {
	f.deleteRequests = append(f.deleteRequests, proto.Clone(req).(*runtimeapi.DeleteRequest))
	filtered := f.containers[:0]
	for _, container := range f.containers {
		if container.GetID() != req.GetID() {
			filtered = append(filtered, container)
		}
	}
	f.containers = filtered
	return &runtimeapi.DeleteResponse{}, nil
}
func (f *fakeSandboxService) Exec(context.Context, *runtimeapi.ExecRequest) (*runtimeapi.ExecResponse, error) {
	return nil, nil
}
func (f *fakeSandboxService) ExecStream(service.ExecStreamServer) error { return nil }
func (f *fakeSandboxService) ProxyHTTP(service.HTTPProxyServer) error   { return nil }
func (f *fakeSandboxService) Wait(context.Context, *runtimeapi.WaitRequest) (*runtimeapi.WaitResponse, error) {
	return nil, nil
}
func (f *fakeSandboxService) List(_ context.Context, req *runtimeapi.ListContainersRequest) (*runtimeapi.ListContainersResponse, error) {
	f.listRequests = append(f.listRequests, proto.Clone(req).(*runtimeapi.ListContainersRequest))
	out := make([]*runtimeapi.ContainerStatus, 0, len(f.containers))
	for _, container := range f.containers {
		if req.GetID() != "" && container.GetID() != req.GetID() {
			continue
		}
		out = append(out, container)
	}
	return &runtimeapi.ListContainersResponse{Containers: out}, nil
}
func (f *fakeSandboxService) Stats(context.Context, *runtimeapi.StatsRequest) (*runtimeapi.StatsResponse, error) {
	return nil, nil
}
func (f *fakeSandboxService) Kill(context.Context, *runtimeapi.KillRequest) (*runtimeapi.KillResponse, error) {
	return nil, nil
}
func (f *fakeSandboxService) Checkpoint(context.Context, *runtimeapi.CheckpointRequest) (*runtimeapi.CheckpointResponse, error) {
	return nil, nil
}
func (f *fakeSandboxService) Version(context.Context, *runtimeapi.VersionRequest) (*runtimeapi.VersionResponse, error) {
	return nil, nil
}
func (f *fakeSandboxService) Ready() bool { return f.ready }
func (f *fakeSandboxService) ReportAllocationStatus(string, int64, commonv1.AllocationStatus, int32, bool, bool, string, string, time.Time) {
}
func (f *fakeSandboxService) RuntimeStatuses() []service.RuntimeStatus { return f.runtimeStatuses }
func (f *fakeSandboxService) NodeInventory() (nodeinventory.NodeInventorySnapshot, bool) {
	return f.inventory, f.inventoryReady
}

func TestHTTPInventoryzNotReady(t *testing.T) {
	svc := &fakeSandboxService{inventory: nodeinventory.NewSnapshot()}
	mux := NewHTTPMux(svc, &NginxDashboard{})

	req := httptest.NewRequest(http.MethodGet, "/inventoryz", nil)
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d", resp.Code, http.StatusServiceUnavailable)
	}
}

func TestHTTPInventoryzReady(t *testing.T) {
	snapshot := nodeinventory.NewSnapshot()
	snapshot.Node.Name = "node-a"
	snapshot.Sources["axnoded"] = nodeinventory.SourceStatus{Status: nodeinventory.StatusReady}
	svc := &fakeSandboxService{
		inventory:      snapshot,
		inventoryReady: true,
	}
	mux := NewHTTPMux(svc, &NginxDashboard{})

	req := httptest.NewRequest(http.MethodGet, "/inventoryz", nil)
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", resp.Code, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var decoded nodeinventory.NodeInventorySnapshot
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if decoded.Node.Name != "node-a" {
		t.Fatalf("node name = %s, want node-a", decoded.Node.Name)
	}
}

func TestRootPageIncludesDiagnosticLinks(t *testing.T) {
	svc := &fakeSandboxService{inventory: nodeinventory.NewSnapshot()}
	mux := NewHTTPMux(svc, &NginxDashboard{})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	resp := httptest.NewRecorder()
	mux.ServeHTTP(resp, req)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	for _, path := range []string{"/inventoryz", "/control-planez", "/debug/metricsz"} {
		if !strings.Contains(string(body), path) {
			t.Fatalf("root page missing %s link: %s", path, string(body))
		}
	}
}

func TestHTTPMetricsDebugSnapshotReplacesLegacyPrometheusEndpoint(t *testing.T) {
	axmetrics.ResetForTest()
	axmetrics.RecordStartResult("cold", "runsc", "local", "ok")
	mux := NewHTTPMux(&fakeSandboxService{inventory: nodeinventory.NewSnapshot()}, &NginxDashboard{})

	debugResponse := httptest.NewRecorder()
	mux.ServeHTTP(debugResponse, httptest.NewRequest(http.MethodGet, "/debug/metricsz", nil))
	if debugResponse.Code != http.StatusOK {
		t.Fatalf("debug metrics status code = %d, want %d", debugResponse.Code, http.StatusOK)
	}
	if got := debugResponse.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("debug metrics cache control = %q, want no-store", got)
	}
	var snapshot axmetrics.Snapshot
	if err := json.NewDecoder(debugResponse.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode debug metrics snapshot: %v", err)
	}
	if len(snapshot.Points) == 0 {
		t.Fatal("debug metrics snapshot has no points")
	}

	legacyResponse := httptest.NewRecorder()
	mux.ServeHTTP(legacyResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if legacyResponse.Code != http.StatusNotFound {
		t.Fatalf("legacy metrics status code = %d, want %d", legacyResponse.Code, http.StatusNotFound)
	}

	writeResponse := httptest.NewRecorder()
	mux.ServeHTTP(writeResponse, httptest.NewRequest(http.MethodPost, "/debug/metricsz", nil))
	if writeResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("debug metrics POST status code = %d, want %d", writeResponse.Code, http.StatusMethodNotAllowed)
	}
	if got := writeResponse.Header().Get("Allow"); got != http.MethodGet {
		t.Fatalf("debug metrics Allow header = %q, want GET", got)
	}
}

func TestHTTPControlPlaneReporterHealth(t *testing.T) {
	svc := &fakeSandboxService{
		controlPlaneHealth: service.ControlPlaneReporterHealth{
			Enabled: true,
			AllocationStatus: service.AllocationStatusReporterHealth{
				Status:              "retrying",
				Pending:             3,
				ConsecutiveFailures: 2,
				RetryDelaySec:       0.2,
			},
		},
	}
	mux := NewHTTPMux(svc, &NginxDashboard{})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/control-planez", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", response.Code, http.StatusOK)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("cache control = %q, want no-store", got)
	}
	var health service.ControlPlaneReporterHealth
	if err := json.NewDecoder(response.Body).Decode(&health); err != nil {
		t.Fatalf("decode health: %v", err)
	}
	if !health.Enabled || health.AllocationStatus.Status != "retrying" || health.AllocationStatus.Pending != 3 {
		t.Fatalf("control-plane health = %#v", health)
	}

	writeResponse := httptest.NewRecorder()
	mux.ServeHTTP(writeResponse, httptest.NewRequest(http.MethodPost, "/control-planez", nil))
	if writeResponse.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status code = %d, want %d", writeResponse.Code, http.StatusMethodNotAllowed)
	}
}

func TestDashboardPageRendersDemoControls(t *testing.T) {
	svc := &fakeSandboxService{inventory: nodeinventory.NewSnapshot()}
	dashboard := &NginxDashboard{svc: svc, natBackend: "iptables"}

	req := httptest.NewRequest(http.MethodGet, "/demo/nginx", nil)
	resp := httptest.NewRecorder()
	dashboard.serveHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d", resp.Code, http.StatusOK)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	page := string(body)
	for _, snippet := range []string{"Managed Sandbox", "value=\"start\"", "value=\"stop\""} {
		if !strings.Contains(page, snippet) {
			t.Fatalf("page missing %q: %s", snippet, page)
		}
	}
}

func TestDashboardStartActionCreatesManagedSandbox(t *testing.T) {
	svc := &fakeSandboxService{}
	dashboard := &NginxDashboard{svc: svc, natBackend: "iptables"}
	spec, ok := demonginx.ManagedSpec("runsc")
	if !ok {
		t.Fatal("missing runsc managed spec")
	}

	req := httptest.NewRequest(http.MethodPost, "/demo/nginx", strings.NewReader("runtime=runsc&action=start"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()
	dashboard.serveHTTP(resp, req)

	if resp.Code != http.StatusSeeOther {
		t.Fatalf("status code = %d, want %d", resp.Code, http.StatusSeeOther)
	}
	if len(svc.startRequests) != 1 {
		t.Fatalf("start request count = %d, want 1", len(svc.startRequests))
	}
	startReq := svc.startRequests[0]
	if startReq.GetContainerID() != spec.SandboxID {
		t.Fatalf("container id = %q, want %q", startReq.GetContainerID(), spec.SandboxID)
	}
	if startReq.GetRuntimeTemplate().GetSandbox() != spec.RuntimeName {
		t.Fatalf("runtime = %q, want %q", startReq.GetRuntimeTemplate().GetSandbox(), spec.RuntimeName)
	}
	if startReq.GetRuntimeTemplate().GetRootfs().GetPath() != spec.RootfsPath {
		t.Fatalf("rootfs path = %q, want %q", startReq.GetRuntimeTemplate().GetRootfs().GetPath(), spec.RootfsPath)
	}
	if got := startReq.GetRuntimeTemplate().GetRuntimeEnvs()["PATH"]; got == "" {
		t.Fatal("expected PATH env to be set")
	}
	if len(startReq.GetPorts()) != 1 || startReq.GetPorts()[0] != "tcp:18080:80" {
		t.Fatalf("ports = %v, want [tcp:18080:80]", startReq.GetPorts())
	}
}

func TestDashboardStopActionDeletesManagedSandbox(t *testing.T) {
	svc := &fakeSandboxService{
		containers: []*runtimeapi.ContainerStatus{
			{ID: "dashboard-nginx-runsc", State: runtimeapi.ContainerState_CONTAINER_RUNNING},
		},
	}
	dashboard := &NginxDashboard{svc: svc, natBackend: "iptables"}

	req := httptest.NewRequest(http.MethodPost, "/demo/nginx", strings.NewReader("runtime=runsc&action=stop"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()
	dashboard.serveHTTP(resp, req)

	if resp.Code != http.StatusSeeOther {
		t.Fatalf("status code = %d, want %d", resp.Code, http.StatusSeeOther)
	}
	if len(svc.deleteRequests) != 1 {
		t.Fatalf("delete request count = %d, want 1", len(svc.deleteRequests))
	}
	if svc.deleteRequests[0].GetID() != "dashboard-nginx-runsc" {
		t.Fatalf("delete id = %q, want dashboard-nginx-runsc", svc.deleteRequests[0].GetID())
	}
}

func TestDashboardRejectsUnsupportedAction(t *testing.T) {
	svc := &fakeSandboxService{}
	dashboard := &NginxDashboard{svc: svc, natBackend: "iptables"}

	req := httptest.NewRequest(http.MethodPost, "/demo/nginx", strings.NewReader("runtime=runsc&action=explode"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp := httptest.NewRecorder()
	dashboard.serveHTTP(resp, req)

	if resp.Code != http.StatusSeeOther {
		t.Fatalf("status code = %d, want %d", resp.Code, http.StatusSeeOther)
	}
	location := resp.Header().Get("Location")
	if !strings.Contains(location, "unsupported+action") {
		t.Fatalf("location = %q, want unsupported action redirect", location)
	}
}
