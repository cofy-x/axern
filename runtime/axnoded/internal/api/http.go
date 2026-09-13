package api

import (
	"encoding/json"
	"net/http"

	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	metrics "github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
)

type httpService interface {
	Ready() bool
	NodeInventory() (nodeinventory.NodeInventorySnapshot, bool)
}

type controlPlaneReporterHealthProvider interface {
	ControlPlaneReporterHealth() service.ControlPlaneReporterHealth
}

func NewHTTPMux(svc httpService) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !svc.Ready() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/inventoryz", func(w http.ResponseWriter, _ *http.Request) {
		snapshot, ready := svc.NodeInventory()
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if !ready {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(snapshot)
	})
	mux.HandleFunc("/control-planez", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		provider, ok := svc.(controlPlaneReporterHealthProvider)
		if !ok {
			http.Error(w, "control-plane reporter health unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(provider.ControlPlaneReporterHealth())
	})
	mux.HandleFunc("/debug/metricsz", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(metrics.SnapshotCurrent())
	})
	return mux
}
