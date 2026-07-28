// Package server implements the Axrun HTTP API server.
// Tier-1 exposes POST /v1/rollouts with SSE streaming and a bounded
// concurrency semaphore to limit parallel rollout execution.
package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	rolloutapp "github.com/cofy-x/axern/apps/axrun/internal/application/rollout"
	"github.com/cofy-x/axern/apps/axrun/internal/application/rundetail"
	"github.com/cofy-x/axern/apps/axrun/internal/contract"
	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

const (
	apiVersion         = "v1"
	defaultMaxRollouts = 4
	maxRequestBodySize = 1 << 20 // 1 MiB
)

// Config holds server configuration.
type Config struct {
	Port        int
	MaxRollouts int
	Output      string
	AuthToken   string
}

func (c *Config) setDefaults() {
	if c.Port <= 0 {
		c.Port = 8080
	}
	if c.MaxRollouts <= 0 {
		c.MaxRollouts = defaultMaxRollouts
	}
	if c.Output == "" {
		c.Output = ".axrun/runs"
	}
}

// Server is the Axrun HTTP API server.
type Server struct {
	config    Config
	service   rolloutapp.Service
	semaphore chan struct{}
}

// New creates a new Server.
func New(config Config, service rolloutapp.Service) *Server {
	config.setDefaults()
	return &Server{
		config:    config,
		service:   service,
		semaphore: make(chan struct{}, config.MaxRollouts),
	}
}

// ListenAndServe starts the HTTP server and blocks until the context is cancelled.
func (s *Server) ListenAndServe(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/rollouts", s.handleRollouts)
	mux.HandleFunc("/v1/runs/", s.handleRunByID)

	addr := fmt.Sprintf(":%d", s.config.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 0, // SSE streams are long-lived
		IdleTimeout:  120 * time.Second,
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Printf("axrun server listening on %s (max_rollouts=%d)", addr, s.config.MaxRollouts)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

// handleHealth responds with a simple health status.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"version": apiVersion,
	})
}

// rolloutOutcome carries the result of a completed rollout goroutine.
type rolloutOutcome struct {
	result rolloutapp.Result
	err    error
}

// handleRollouts handles POST /v1/rollouts.
// It acquires a concurrency slot for the lifetime of the rollout (not just
// the HTTP connection), streams SSE heartbeats and a completion event, and
// allows the client to disconnect without aborting the rollout.
func (s *Server) handleRollouts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeRolloutHTTPError(w, rolloutHTTPErrorParams{
			Status:    http.StatusMethodNotAllowed,
			Code:      domain.RolloutErrorInputInvalid,
			Message:   "method not allowed",
			Phase:     domain.RolloutPhasePlanning,
			Component: "request",
		})
		return
	}
	if !s.authorizeRollout(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		writeRolloutHTTPError(w, rolloutHTTPErrorParams{
			Status:    http.StatusUnauthorized,
			Code:      domain.RolloutErrorInputInvalid,
			Message:   "unauthorized",
			Phase:     domain.RolloutPhasePlanning,
			Component: "auth",
		})
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeRolloutHTTPError(w, rolloutHTTPErrorParams{
			Status:    http.StatusInternalServerError,
			Code:      domain.RolloutErrorInfrastructureFailure,
			Message:   "streaming not supported",
			Phase:     domain.RolloutPhasePlanning,
			Component: "server",
			Retriable: true,
		})
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
	var req RolloutRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		writeRolloutHTTPError(w, rolloutHTTPErrorParams{
			Status:    http.StatusBadRequest,
			Code:      domain.RolloutErrorInputInvalid,
			Message:   fmt.Sprintf("invalid request body: %v", err),
			Phase:     domain.RolloutPhasePlanning,
			Component: "request",
		})
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeRolloutHTTPError(w, rolloutHTTPErrorParams{
			Status:    http.StatusBadRequest,
			Code:      domain.RolloutErrorInputInvalid,
			Message:   "request body must contain exactly one JSON value",
			Phase:     domain.RolloutPhasePlanning,
			Component: "request",
		})
		return
	}
	if err := req.validate(); err != nil {
		writeRolloutHTTPError(w, rolloutHTTPErrorParams{
			Status:    http.StatusBadRequest,
			Code:      domain.RolloutErrorInputInvalid,
			Message:   err.Error(),
			Phase:     domain.RolloutPhasePlanning,
			Component: "request",
		})
		return
	}

	// Apply server-level defaults.
	if req.Output == "" {
		req.Output = s.config.Output
	} else if filepath.Clean(req.Output) != filepath.Clean(s.config.Output) {
		writeRolloutHTTPError(w, rolloutHTTPErrorParams{
			Status:    http.StatusBadRequest,
			Code:      domain.RolloutErrorInputInvalid,
			Message:   "output override is not supported by the HTTP server",
			Phase:     domain.RolloutPhasePlanning,
			Component: "request",
		})
		return
	}
	if strings.TrimSpace(req.ResumeRunDir) != "" {
		if !isPathWithinBase(s.config.Output, req.ResumeRunDir) {
			writeRolloutHTTPError(w, rolloutHTTPErrorParams{
				Status:    http.StatusBadRequest,
				Code:      domain.RolloutErrorInputInvalid,
				Message:   "resume_run_dir must be under the server output directory",
				Phase:     domain.RolloutPhasePlanning,
				Component: "request",
			})
			return
		}
	}
	if req.RunID == "" && strings.TrimSpace(req.ResumeRunDir) == "" {
		generatedRunID, err := domain.NewRolloutRunID(time.Now().UTC())
		if err != nil {
			writeRolloutHTTPError(w, rolloutHTTPErrorParams{
				Status:    http.StatusInternalServerError,
				Code:      domain.RolloutErrorInfrastructureFailure,
				Message:   fmt.Sprintf("generate run id: %v", err),
				Phase:     domain.RolloutPhasePlanning,
				Component: "server",
				Retriable: true,
			})
			return
		}
		req.RunID = generatedRunID
	}
	params, err := rolloutapp.NormalizeParams(req.toParams())
	if err != nil {
		writeRolloutHTTPError(w, rolloutHTTPErrorParams{
			Status:    http.StatusBadRequest,
			Code:      domain.RolloutErrorInputInvalid,
			Message:   err.Error(),
			Phase:     domain.RolloutPhasePlanning,
			Component: "request",
		})
		return
	}
	eventRunID := rolloutEventRunID(params)
	phaseEvents := make(chan domain.PhaseEvent, 64)
	params.PhaseReporter = func(event domain.PhaseEvent) {
		if event.RunID == "" {
			event.RunID = eventRunID
		}
		select {
		case phaseEvents <- event:
		default:
		}
	}

	// Acquire a concurrency slot for every accepted rollout request.
	// Layout-only (execute=false) requests still perform non-trivial planning and
	// filesystem work, so they should be bounded to avoid resource exhaustion.
	select {
	case s.semaphore <- struct{}{}:
	default:
		w.Header().Set("Retry-After", "1")
		writeRolloutHTTPError(w, rolloutHTTPErrorParams{
			Status:    http.StatusServiceUnavailable,
			Code:      domain.RolloutErrorInfrastructureFailure,
			Message:   fmt.Sprintf("too many concurrent rollouts (limit %d)", s.config.MaxRollouts),
			Phase:     domain.RolloutPhasePlanning,
			Component: "server",
			Retriable: true,
		})
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	if eventRunID != "" {
		w.Header().Set("X-Run-ID", eventRunID)
	}
	w.WriteHeader(http.StatusOK)
	if eventRunID != "" {
		acceptedStatus := domain.RunStatusCreated
		if params.ResumeRunDir != "" {
			acceptedStatus = domain.RunStatusRunning
		}
		writeSSEEvent(w, flusher, "run.accepted", map[string]any{
			"run_id": eventRunID,
			"status": acceptedStatus,
		})
	}

	// Run the rollout in a goroutine; the semaphore slot is released only
	// after the rollout completes so the concurrency limit reflects actual
	// in-flight work even after a client disconnect.
	done := make(chan rolloutOutcome, 1)
	go func() {
		outcome := rolloutOutcome{}
		defer func() {
			if recovered := recover(); recovered != nil {
				outcome = rolloutOutcome{
					err: fmt.Errorf("rollout panic: %v", recovered),
				}
			}
			close(phaseEvents)
			<-s.semaphore
			done <- outcome
		}()
		outcome.result, outcome.err = s.service.Run(params)
	}()

	// Stream SSE until the rollout finishes or the client disconnects.
	// Heartbeat comments keep the connection alive for long rollouts.
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	var lastPhaseError *domain.RolloutError
	for {
		select {
		case event, ok := <-phaseEvents:
			if !ok {
				phaseEvents = nil
				continue
			}
			rememberPhaseError(event, &lastPhaseError)
			writeSSEEvent(w, flusher, "run.phase", event)
		case outcome := <-done:
			drainPhaseEvents(w, flusher, phaseEvents, &lastPhaseError)
			if outcome.err != nil {
				rolloutErr := terminalRolloutError(outcome.err, lastPhaseError)
				event := map[string]any{
					"error": rolloutErr,
				}
				if eventRunID != "" {
					event["run_id"] = eventRunID
				}
				writeSSEEvent(w, flusher, "run.failed", event)
			} else {
				writeSSEEvent(w, flusher, "run.completed", rolloutResultEvent(outcome.result))
			}
			return
		case <-ticker.C:
			writeSSEComment(w, flusher, "heartbeat")
		case <-r.Context().Done():
			// Client disconnected; rollout continues in background and
			// will release the semaphore slot when it finishes.
			return
		}
	}
}

func drainPhaseEvents(w http.ResponseWriter, flusher http.Flusher, phaseEvents <-chan domain.PhaseEvent, lastPhaseError **domain.RolloutError) {
	if phaseEvents == nil {
		return
	}
	for event := range phaseEvents {
		rememberPhaseError(event, lastPhaseError)
		writeSSEEvent(w, flusher, "run.phase", event)
	}
}

func rememberPhaseError(event domain.PhaseEvent, lastPhaseError **domain.RolloutError) {
	if event.Status != domain.PhaseStatusFailed || event.Error == nil {
		return
	}
	rolloutErr := *event.Error
	*lastPhaseError = &rolloutErr
}

func terminalRolloutError(err error, lastPhaseError *domain.RolloutError) domain.RolloutError {
	if lastPhaseError == nil {
		return rolloutapp.RolloutError(err, domain.RolloutPhasePlanning)
	}
	rolloutErr := *lastPhaseError
	if err != nil {
		rolloutErr.Message = err.Error()
	}
	return rolloutErr
}

// handleRunByID returns run envelope status for a known run id.
func (s *Server) handleRunByID(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.authorizeRollout(r) {
		w.Header().Set("WWW-Authenticate", "Bearer")
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	runID := strings.TrimPrefix(r.URL.Path, "/v1/runs/")
	runID = strings.TrimSpace(runID)
	if runID == "" {
		http.Error(w, "run id is required", http.StatusBadRequest)
		return
	}
	if err := contract.ValidatePathSegment("rollout run id", runID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	runDir := filepath.Join(s.config.Output, runID)
	runJSONPath := filepath.Join(runDir, "run.json")
	if _, err := os.Stat(runJSONPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "run not found", http.StatusNotFound)
			return
		}
		http.Error(w, fmt.Sprintf("stat run: %v", err), http.StatusInternalServerError)
		return
	}
	detail, err := rundetail.Load(runDir)
	if err != nil {
		http.Error(w, fmt.Sprintf("read run detail: %v", err), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(detail)
}

func (s *Server) authorizeRollout(r *http.Request) bool {
	token := strings.TrimSpace(s.config.AuthToken)
	if token == "" {
		return true
	}
	const bearerPrefix = "Bearer "
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(authHeader, bearerPrefix) {
		return false
	}
	presented := strings.TrimSpace(strings.TrimPrefix(authHeader, bearerPrefix))
	if len(presented) != len(token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(token)) == 1
}

func isPathWithinBase(base string, candidate string) bool {
	if strings.TrimSpace(base) == "" || strings.TrimSpace(candidate) == "" {
		return false
	}
	baseAbs, err := resolvePathForContainment(base)
	if err != nil {
		return false
	}
	candidateAbs, err := resolvePathForContainment(candidate)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(baseAbs, candidateAbs)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

func resolvePathForContainment(path string) (string, error) {
	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolvedPath, err := filepath.EvalSymlinks(absolutePath)
	if err == nil {
		return resolvedPath, nil
	}
	if os.IsNotExist(err) {
		ancestor := absolutePath
		for {
			if _, statErr := os.Stat(ancestor); statErr == nil {
				break
			}
			next := filepath.Dir(ancestor)
			if next == ancestor {
				return absolutePath, nil
			}
			ancestor = next
		}
		resolvedAncestor, resolveErr := filepath.EvalSymlinks(ancestor)
		if resolveErr != nil {
			return absolutePath, nil
		}
		relativeTail, relErr := filepath.Rel(ancestor, absolutePath)
		if relErr != nil {
			return absolutePath, nil
		}
		return filepath.Join(resolvedAncestor, relativeTail), nil
	}
	return "", err
}

func rolloutEventRunID(params rolloutapp.Params) string {
	if params.RunID != "" {
		return params.RunID
	}
	if strings.TrimSpace(params.ResumeRunDir) == "" {
		return ""
	}
	runID := filepath.Base(filepath.Clean(params.ResumeRunDir))
	if err := contract.ValidatePathSegment("rollout run id", runID); err != nil {
		return ""
	}
	return runID
}

func writeSSEEvent(w http.ResponseWriter, flusher http.Flusher, event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		log.Printf("axrun server: marshal SSE event %q failed: %v", event, err)
		payload = []byte(`{"error":"failed to encode event payload"}`)
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
		log.Printf("axrun server: write SSE event %q failed: %v", event, err)
		return
	}
	flusher.Flush()
}

func writeSSEComment(w http.ResponseWriter, flusher http.Flusher, comment string) {
	if _, err := fmt.Fprintf(w, ": %s\n\n", comment); err != nil {
		log.Printf("axrun server: write SSE heartbeat failed: %v", err)
		return
	}
	flusher.Flush()
}

type rolloutHTTPErrorParams struct {
	Status    int
	Code      domain.RolloutErrorCode
	Message   string
	Phase     domain.RolloutPhase
	Component string
	Retriable bool
}

func writeRolloutHTTPError(w http.ResponseWriter, params rolloutHTTPErrorParams) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(params.Status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": domain.RolloutError{
			Code:      params.Code,
			Message:   params.Message,
			Phase:     params.Phase,
			Component: params.Component,
			Retriable: params.Retriable,
		},
	})
}

func rolloutResultEvent(result rolloutapp.Result) map[string]any {
	return map[string]any{
		"run_id":        result.RunID,
		"run_dir":       result.RunDir,
		"status":        result.Status,
		"task_count":    result.TaskCount,
		"episode_count": result.EpisodeCount,
		"summary":       result.Summary,
	}
}
