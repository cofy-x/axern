package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/process"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

func (s *Server) handleProcesses(w http.ResponseWriter, r *http.Request) {
	if s.processes == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeUnavailable, "process registry unavailable")
		return
	}
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, http.StatusOK, wireProcessList(s.processes.List()))
	case http.MethodPost:
		s.handleProcessStart(w, r)
	default:
		writeMethodNotAllowed(w)
	}
}

func (s *Server) handleProcessStart(w http.ResponseWriter, r *http.Request) {
	var request process.StartRequest
	if err := decodeRequiredJSONRequest(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, "invalid process request: "+err.Error())
		return
	}
	status, err := s.processes.Start(request)
	if err != nil {
		if errors.Is(err, process.ErrResourceLimit) {
			writeError(w, http.StatusServiceUnavailable, errorCodeUnavailable, err.Error())
			return
		}
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, wireProcessStatus(status))
}

func (s *Server) handleProcess(w http.ResponseWriter, r *http.Request) {
	if s.processes == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeUnavailable, "process registry unavailable")
		return
	}
	id, action, ok := splitProcessPath(r.URL.Path)
	if !ok {
		writeNotFound(w)
		return
	}
	switch action {
	case "":
		s.handleProcessStatus(w, r, id)
	case "signal":
		s.handleProcessSignal(w, r, id)
	case "wait":
		s.handleProcessWait(w, r, id)
	case "stdin":
		s.handleProcessStdin(w, r, id)
	case "stdin-close":
		s.handleProcessStdinClose(w, r, id)
	case "stream":
		s.handleProcessStream(w, r, id)
	case "resize":
		s.handleProcessResize(w, r, id)
	default:
		writeNotFound(w)
	}
}

func (s *Server) handleProcessStatus(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	status, ok := s.processes.Status(id)
	if !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, wireProcessStatus(status))
}

func (s *Server) handleProcessSignal(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var request process.SignalRequest
	if err := decodeRequiredJSONRequest(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, "invalid signal request: "+err.Error())
		return
	}
	signal, err := process.ParseSignal(request.Signal)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, err.Error())
		return
	}
	status, ok, err := s.processes.Signal(id, signal)
	if err != nil {
		writeError(w, http.StatusInternalServerError, errorCodeInternal, err.Error())
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, wireProcessStatus(status))
}

func (s *Server) handleProcessWait(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	status, ok, err := s.processes.Wait(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusRequestTimeout, errorCodeTimeout, err.Error())
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, wireProcessStatus(status))
}

func (s *Server) handleProcessStdin(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var request process.StdinRequest
	if err := decodeRequiredJSONRequest(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, "invalid stdin request: "+err.Error())
		return
	}
	status, ok, err := s.processes.WriteStdin(id, request.Data)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, err.Error())
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, wireProcessStatus(status))
}

func (s *Server) handleProcessStdinClose(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	status, ok, err := s.processes.CloseStdin(id)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, err.Error())
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, wireProcessStatus(status))
}

func (s *Server) handleProcessStream(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodGet {
		writeMethodNotAllowed(w)
		return
	}
	events, ok, err := s.processes.SubscribeOutput(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, err.Error())
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	encoder := json.NewEncoder(w)
	for event := range events {
		if err := encoder.Encode(event); err != nil {
			return
		}
		if flusher != nil {
			flusher.Flush()
		}
	}
}

func (s *Server) handleProcessResize(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var request process.ResizeRequest
	if err := decodeRequiredJSONRequest(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, "invalid resize request: "+err.Error())
		return
	}
	status, ok, err := s.processes.Resize(id, request.Cols, request.Rows)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, err.Error())
		return
	}
	if !ok {
		writeNotFound(w)
		return
	}
	writeJSON(w, http.StatusOK, wireProcessStatus(status))
}

func splitProcessPath(path string) (string, string, bool) {
	rest := strings.TrimPrefix(path, wire.PathProcessesPrefix)
	if rest == path || rest == "" {
		return "", "", false
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 1 {
		return parts[0], "", parts[0] != ""
	}
	if len(parts) == 2 {
		return parts[0], parts[1], parts[0] != "" && parts[1] != ""
	}
	return "", "", false
}
