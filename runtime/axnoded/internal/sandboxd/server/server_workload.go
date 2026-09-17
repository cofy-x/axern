package server

import (
	"net/http"
	"strconv"
	"strings"
	"syscall"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/workload"
)

func (s *Server) handleWorkloadStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.workload == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeUnavailable, "workload supervisor unavailable")
		return
	}
	var request wire.WorkloadStopRequest
	if err := decodeJSONRequest(r, &request, true); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, "invalid workload stop request: "+err.Error())
		return
	}
	signal, err := workloadSignal(request.Signal)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, wireWorkloadExit(s.workload.Shutdown(signal)))
}

func (s *Server) handleWorkloadSignal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.workload == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeUnavailable, "workload supervisor unavailable")
		return
	}
	var request wire.WorkloadStopRequest
	if err := decodeJSONRequest(r, &request, true); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, "invalid workload signal request: "+err.Error())
		return
	}
	signal, err := workloadSignal(request.Signal)
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, err.Error())
		return
	}
	if err := s.workload.Signal(signal); err != nil {
		writeError(w, http.StatusConflict, errorCodeUnavailable, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct{}{})
}

func (s *Server) handleWorkloadWait(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	if s.workload == nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeUnavailable, "workload supervisor unavailable")
		return
	}
	result, err := s.workload.Wait(r.Context())
	if err != nil {
		return
	}
	writeJSON(w, http.StatusOK, wireWorkloadExit(result))
}

func wireWorkloadExit(result workload.ProcessResult) wire.WorkloadExitResponse {
	signal := ""
	if result.Signal != nil {
		signal = result.Signal.String()
	}
	lastError := ""
	if result.Err != nil {
		lastError = result.Err.Error()
	}
	return wire.WorkloadExitResponse{ExitCode: result.ExitCode, Signal: signal, ExitedAt: result.FinishedAt, LastError: lastError}
}

func workloadSignal(value string) (syscall.Signal, error) {
	normalized := strings.TrimPrefix(strings.ToUpper(strings.TrimSpace(value)), "SIG")
	if number, err := strconv.Atoi(normalized); err == nil && number > 0 {
		return syscall.Signal(number), nil
	}
	switch normalized {
	case "HUP":
		return syscall.SIGHUP, nil
	case "INT":
		return syscall.SIGINT, nil
	case "QUIT":
		return syscall.SIGQUIT, nil
	case "KILL":
		return syscall.SIGKILL, nil
	case "TERM", "":
		return syscall.SIGTERM, nil
	case "USR1":
		return syscall.SIGUSR1, nil
	case "USR2":
		return syscall.SIGUSR2, nil
	default:
		return 0, &signalError{value: value}
	}
}

type signalError struct{ value string }

func (e *signalError) Error() string { return "unsupported workload signal " + strconv.Quote(e.value) }
