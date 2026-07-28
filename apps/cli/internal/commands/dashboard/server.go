package dashboard

import (
	"embed"
	"io/fs"
	"net/http"
	"time"

	appdashboard "github.com/cofy-x/axern/apps/cli/internal/application/dashboard"
	apptunnel "github.com/cofy-x/axern/apps/cli/internal/application/tunnel"
)

//go:embed ui/*
var uiFS embed.FS

type server struct {
	dashboard     appdashboard.Control
	serviceClient appdashboardServiceClient
	linksConfig   appdashboard.LinksConfig
	refresh       time.Duration
}

type appdashboardServiceClient interface {
	apptunnel.ServiceClient
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/summary", s.handleSummary)
	mux.HandleFunc("/api/reconcile-health", s.handleReconcileHealth)
	mux.HandleFunc("/api/admin", s.handleAdmin)
	mux.HandleFunc("/api/admin/allocation-retries/", s.handleAdminAllocationRetryPath)
	mux.HandleFunc("/api/quotas", s.handleQuotas)
	mux.HandleFunc("/api/quotas/", s.handleQuotaPath)
	mux.HandleFunc("/api/services", s.handleServices)
	mux.HandleFunc("/api/services/", s.handleServicePath)
	mux.HandleFunc("/api/tunnels", s.handleTunnels)
	mux.HandleFunc("/api/tunnels/", s.handleTunnelPath)
	mux.HandleFunc("/api/tunnel-doctor", s.handleTunnelDoctor)
	mux.HandleFunc("/api/links", s.handleLinks)

	staticFS, _ := fs.Sub(uiFS, "ui")
	fileServer := http.FileServer(http.FS(staticFS))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" || r.URL.Path == "/index.html" {
			s.handleIndex(w, r)
			return
		}
		fileServer.ServeHTTP(w, r)
	})
	return mux
}
