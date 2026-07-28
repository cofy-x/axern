package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/browser"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/computeruse"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/diagnostic"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/fileapi"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/process"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/provider"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/workload"
)

type Server struct {
	state       *workload.State
	processes   *process.Registry
	files       *fileapi.Service
	computerUse *computeruse.Service
	browser     *browser.Service
	providers   provider.Registry
	mux         *http.ServeMux
}

func New(state *workload.State, processes *process.Registry, waiter *proc.Waiter) *Server {
	files := fileapi.NewService()
	computerUse := computeruse.NewService(computeruse.DetectFromEnv(), waiter)
	browserService := browser.NewService(browser.DetectFromEnv(), waiter)
	providers := provider.New(
		provider.Static(provider.ProviderNameCore, wire.CoreCapabilities()...),
		computerUse.Provider(),
		browserService.Provider(),
	)
	if files != nil {
		providers.Add(provider.Static(provider.ProviderNameFile, wire.FileCapabilities()...))
	}
	if processes != nil {
		providers.Add(provider.Static(provider.ProviderNameProcess, wire.ProcessCapabilities()...))
	}
	server := &Server{state: state, processes: processes, files: files, computerUse: computerUse, browser: browserService, providers: providers, mux: http.NewServeMux()}
	server.mux.HandleFunc(wire.PathHealth, server.handleHealth)
	server.mux.HandleFunc(wire.PathReady, server.handleReady)
	server.mux.HandleFunc(wire.PathCapabilities, server.handleCapabilities)
	server.mux.HandleFunc(wire.PathDiagnostics, server.handleDiagnostics)
	server.mux.HandleFunc(wire.PathMounts, server.handleMounts)
	server.mux.HandleFunc(wire.PathPorts, server.handlePorts)
	server.mux.HandleFunc(wire.PathProbe, server.handleProbe)
	server.mux.HandleFunc(wire.PathStatus, server.handleStatus)
	server.mux.HandleFunc(wire.PathFilesPrefix, server.handleFiles)
	server.mux.HandleFunc(wire.PathProcesses, server.handleProcesses)
	server.mux.HandleFunc(wire.PathProcessesPrefix, server.handleProcess)
	server.mux.HandleFunc(wire.PathComputerUsePrefix, server.handleComputerUse)
	server.mux.HandleFunc(wire.PathBrowserPrefix, server.handleBrowser)
	return server
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) Shutdown(ctx context.Context, processGrace time.Duration) error {
	if s == nil {
		return nil
	}
	if s.browser != nil {
		_, _ = s.browser.Close(ctx)
	}
	if s.processes != nil {
		return s.processes.Shutdown(ctx, processGrace)
	}
	return nil
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, wire.HealthResponse{ProtocolVersion: wire.ProtocolVersion, Status: "ok"})
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, wire.ReadyResponse{ProtocolVersion: wire.ProtocolVersion, Ready: true})
}

func (s *Server) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	snapshot := s.providerSnapshot("summary", nil, nil)
	writeJSON(w, http.StatusOK, wire.CapabilitiesResponse{
		ProtocolVersion: wire.ProtocolVersion,
		Capabilities:    snapshot.Capabilities,
		Providers:       wireProviders(snapshot.Providers),
		Summary:         wireProviderSummary(snapshot.Summary),
	})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, wireStatus(s.state.Status()))
}

func (s *Server) handleDiagnostics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	detail := diagnosticsDetail(r)
	var computerUseStatus *computeruse.StatusResponse
	var browserStatus *browser.StatusResponse
	if detail == "full" && s.computerUse != nil {
		status := s.computerUse.Status(r.Context())
		computerUseStatus = &status
	}
	if detail == "full" && s.browser != nil {
		status := s.browser.Status()
		browserStatus = &status
	}
	providerSnapshot := s.providerSnapshot(detail, computerUseStatus, browserStatus)
	var processes *wire.ProcessListResponse
	var processSummary wire.ProcessSummary
	if s.processes != nil {
		list := s.processes.List()
		wireList := wireProcessList(list)
		processSummary = wireProcessSummary(wireList)
		if detail == "full" {
			processes = &wireList
		}
	}
	var ports *wire.PortSnapshot
	var mounts *wire.MountSnapshot
	var fileLimits *wire.FileLimitSnapshot
	var computerUse *wire.ComputerUseStatusResponse
	var browserWireStatus *wire.BrowserStatusResponse
	if detail == "full" {
		portsSnapshot := wirePortSnapshot(diagnostic.Ports())
		ports = &portsSnapshot
		mountSnapshot := wireMountSnapshot(diagnostic.Mounts())
		mounts = &mountSnapshot
		limitSnapshot := wireFileLimitSnapshot(fileapi.Limits())
		fileLimits = &limitSnapshot
	}
	if computerUseStatus != nil {
		wireStatus := wireComputerUseStatus(*computerUseStatus)
		computerUse = &wireStatus
	}
	if browserStatus != nil {
		wireStatus := wireBrowserStatus(*browserStatus)
		browserWireStatus = &wireStatus
	}
	writeJSON(w, http.StatusOK, wire.DiagnosticsResponse{
		ProtocolVersion: wire.ProtocolVersion,
		GeneratedAt:     time.Now().UTC(),
		Ready:           true,
		Detail:          detail,
		Status:          wireStatus(s.state.Status()),
		Capabilities:    providerSnapshot.Capabilities,
		Providers:       wireProviders(providerSnapshot.Providers),
		ProviderSummary: wireProviderSummary(providerSnapshot.Summary),
		ProcessSummary:  processSummary,
		FileLimits:      fileLimits,
		Processes:       processes,
		Ports:           ports,
		Mounts:          mounts,
		ComputerUse:     computerUse,
		Browser:         browserWireStatus,
	})
}

func (s *Server) providerSnapshot(detail string, computerUseStatus *computeruse.StatusResponse, browserStatus *browser.StatusResponse) provider.Snapshot {
	if detail != "full" {
		return s.providers.Snapshot()
	}
	registry := provider.New(s.providers.Providers()...)
	if computerUseStatus != nil {
		registry.Add(providerFromComputerUseStatus(*computerUseStatus))
	}
	if browserStatus != nil {
		registry.Add(providerFromBrowserStatus(*browserStatus))
	}
	return registry.Snapshot()
}

func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var request wire.ProbeRequest
	if err := decodeRequiredJSONRequest(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, err.Error())
		return
	}
	if err := validateProbeRequest(request); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, err.Error())
		return
	}
	response := diagnostic.Probe(diagnostic.ProbeRequest{
		Host:      request.Host,
		TimeoutMS: request.TimeoutMS,
		HTTP:      diagnosticHTTPProbe(request.HTTP),
		TCP:       diagnosticTCPProbe(request.TCP),
	})
	writeJSON(w, http.StatusOK, wireProbeResponse(response))
}

func (s *Server) handlePorts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, wirePortSnapshot(diagnostic.Ports()))
}

func (s *Server) handleMounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	writeJSON(w, http.StatusOK, wireMountSnapshot(diagnostic.Mounts()))
}

func diagnosticsDetail(r *http.Request) string {
	if r.URL.Query().Get("detail") == "full" {
		return "full"
	}
	return "summary"
}

func validateProbeRequest(request wire.ProbeRequest) error {
	targets := 0
	if request.HTTP != nil {
		targets++
		if request.HTTP.Port <= 0 {
			return errors.New("http probe port must be positive")
		}
	}
	if request.TCP != nil {
		targets++
		if request.TCP.Port <= 0 {
			return errors.New("tcp probe port must be positive")
		}
	}
	if targets != 1 {
		return errors.New("exactly one probe target is required")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", wire.ContentTypeJSON)
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}
