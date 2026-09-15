package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	axmetrics "github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
)

type fakeHTTPService struct {
	ready              bool
	inventory          nodeinventory.NodeInventorySnapshot
	inventoryReady     bool
	controlPlaneHealth service.ControlPlaneReporterHealth
}

func (f *fakeHTTPService) Ready() bool { return f.ready }

func (f *fakeHTTPService) NodeInventory() (nodeinventory.NodeInventorySnapshot, bool) {
	return f.inventory, f.inventoryReady
}

func (f *fakeHTTPService) ControlPlaneReporterHealth() service.ControlPlaneReporterHealth {
	return f.controlPlaneHealth
}

func TestHTTPDoesNotExposeOperatorOrProfilerEndpoints(t *testing.T) {
	mux := NewHTTPMux(&fakeHTTPService{})
	for _, path := range []string{"/", "/demo/nginx", "/debug/pprof/"} {
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("GET %s status = %d, want %d", path, response.Code, http.StatusNotFound)
		}
	}
}

func TestHTTPInventoryReadiness(t *testing.T) {
	snapshot := nodeinventory.NewSnapshot()
	snapshot.Node.Name = "node-a"
	for _, ready := range []bool{false, true} {
		response := httptest.NewRecorder()
		NewHTTPMux(&fakeHTTPService{inventory: snapshot, inventoryReady: ready}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/inventoryz", nil))
		want := http.StatusServiceUnavailable
		if ready {
			want = http.StatusOK
		}
		if response.Code != want {
			t.Fatalf("ready=%t status = %d, want %d", ready, response.Code, want)
		}
	}
}

func TestHTTPMetricsDebugSnapshot(t *testing.T) {
	axmetrics.ResetForTest()
	axmetrics.RecordStartResult("cold", "runsc", "local", "ok")
	response := httptest.NewRecorder()
	NewHTTPMux(&fakeHTTPService{}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/debug/metricsz", nil))
	var snapshot axmetrics.Snapshot
	if err := json.NewDecoder(response.Body).Decode(&snapshot); err != nil {
		t.Fatalf("decode metrics: %v", err)
	}
	if len(snapshot.Points) == 0 {
		t.Fatal("debug metrics snapshot has no points")
	}
}

func TestHTTPControlPlaneHealthRejectsWrites(t *testing.T) {
	response := httptest.NewRecorder()
	NewHTTPMux(&fakeHTTPService{}).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/control-planez", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusMethodNotAllowed)
	}
}
