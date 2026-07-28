package api

import (
	"encoding/json"
	"net/http"
	"net/http/pprof"

	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	metrics "github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service"
	"github.com/cofy-x/axern/runtime/axnoded/version"
)

type rootPageData struct {
	Ready    bool
	Version  string
	Revision string
	Message  string
	Runtimes []service.RuntimeStatus
}

type httpService interface {
	Ready() bool
	RuntimeStatuses() []service.RuntimeStatus
	NodeInventory() (nodeinventory.NodeInventorySnapshot, bool)
}

type controlPlaneReporterHealthProvider interface {
	ControlPlaneReporterHealth() service.ControlPlaneReporterHealth
}

func NewHTTPMux(svc httpService, dashboard *NginxDashboard) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		renderRootPage(w, svc)
	})
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
	mux.Handle("/debug/pprof/", http.HandlerFunc(pprof.Index))
	mux.Handle("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
	mux.Handle("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
	mux.Handle("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
	mux.Handle("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))
	mux.HandleFunc("/demo/nginx", dashboard.serveHTTP)
	return mux
}

func renderRootPage(w http.ResponseWriter, svc httpService) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := rootPageData{
		Ready:    svc.Ready(),
		Version:  version.Version,
		Revision: version.Revision,
		Message:  version.Message,
		Runtimes: svc.RuntimeStatuses(),
	}
	if err := rootPageTemplate.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
