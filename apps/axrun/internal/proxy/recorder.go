package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
	"github.com/cofy-x/axern/apps/axrun/internal/redact"
	"github.com/cofy-x/axern/apps/axrun/internal/runref"
)

const (
	agentRawLogFilename = "agent.raw.jsonl"
	llmArtifactDirname  = "llm"
)

// RecorderResult aggregates telemetry captured during a proxy session.
type RecorderResult struct {
	RawLogRef     string
	LLMDirRef     string
	RequestCount  int
	ResponseCount int
	ErrorCount    int
	Usage         *domain.UsageMetrics
	Artifacts     []domain.ArtifactRef
}

// Recorder writes LLM request/response artifacts and raw event logs
// during a proxy session. It is safe for concurrent use.
type Recorder struct {
	artifactDir string
	llmDir      string
	rawLogPath  string
	provider    Provider

	mu             sync.Mutex
	nextRequestID  int
	nextResponseID int
	nextEventID    int
	firstErr       error
	requests       int
	responses      int
	errors         int
	usage          domain.UsageMetrics
}

// NewRecorder creates a Recorder that writes artifacts under artifactDir.
// Returns (nil, nil) when artifactDir is empty (telemetry disabled).
func NewRecorder(artifactDir string, provider Provider) (*Recorder, error) {
	if strings.TrimSpace(artifactDir) == "" {
		return nil, nil
	}
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		return nil, fmt.Errorf("create agent artifact directory: %w", err)
	}
	if err := os.Chmod(artifactDir, 0o700); err != nil {
		return nil, fmt.Errorf("protect agent artifact directory: %w", err)
	}
	rawLogPath := filepath.Join(artifactDir, agentRawLogFilename)
	file, err := os.OpenFile(rawLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("create agent raw log: %w", err)
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("protect agent raw log: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close agent raw log: %w", err)
	}
	return &Recorder{artifactDir: artifactDir, rawLogPath: rawLogPath, provider: provider}, nil
}

// RecordRequest captures a proxied HTTP request and returns a request ID.
func (r *Recorder) RecordRequest(req *http.Request) int {
	if r == nil || req == nil {
		return 0
	}
	body, err := ReadAndRestoreBody(req)
	if err != nil {
		r.RecordError("llm.error", req.Method, req.URL.Path, err)
		return 0
	}
	requestID := r.nextRequest()
	requestRef := RequestRef(requestID)
	bodyRef := ""
	if len(body) > 0 {
		llmDir, ok := r.ensureLLMDir()
		if !ok {
			return requestID
		}
		path := filepath.Join(llmDir, fmt.Sprintf("request-%06d.body", requestID))
		if err := os.WriteFile(path, body, 0o600); err != nil {
			r.setErr(fmt.Errorf("write LLM request body: %w", err))
		} else if err := os.Chmod(path, 0o600); err != nil {
			r.setErr(fmt.Errorf("protect LLM request body: %w", err))
		} else {
			bodyRef = runRelativeArtifactPath(r.artifactDir, path)
		}
	}
	timestamp := time.Now().UTC()
	model := ""
	if r.provider != nil {
		model = r.provider.ExtractModel(body)
	}
	event := domain.AgentRawEvent{
		Type:       domain.AgentRawEventLLMRequest,
		Timestamp:  &timestamp,
		Method:     req.Method,
		Path:       req.URL.EscapedPath(),
		Model:      model,
		Headers:    SanitizeHeaders(req.Header),
		BodyRef:    bodyRef,
		RequestRef: requestRef,
	}
	r.appendEvent(event)
	if r.provider != nil && r.provider.IsInferenceRequest(req.Method, req.URL.Path) {
		r.mu.Lock()
		r.requests++
		r.mu.Unlock()
	}
	return requestID
}

// RecordResponseStart captures response headers.
func (r *Recorder) RecordResponseStart(req *http.Request, resp *http.Response, requestID int, responseID int) {
	if r == nil || req == nil || resp == nil {
		return
	}
	timestamp := time.Now().UTC()
	r.appendEvent(domain.AgentRawEvent{
		Type:        domain.AgentRawEventLLMResponse,
		Timestamp:   &timestamp,
		Method:      req.Method,
		Path:        req.URL.EscapedPath(),
		Status:      resp.StatusCode,
		Headers:     SanitizeHeaders(resp.Header),
		RequestRef:  RequestRef(requestID),
		ResponseRef: ResponseRef(responseID),
	})
}

// RecordResponseChunk captures a single SSE chunk.
func (r *Recorder) RecordResponseChunk(req *http.Request, requestID int, responseID int, chunkIndex int, chunk []byte) {
	if r == nil || req == nil || len(chunk) == 0 {
		return
	}
	llmDir, ok := r.ensureLLMDir()
	if !ok {
		return
	}
	path := filepath.Join(llmDir, fmt.Sprintf("response-%06d-chunk-%06d.body", responseID, chunkIndex))
	if err := os.WriteFile(path, chunk, 0o600); err != nil {
		r.setErr(fmt.Errorf("write LLM response chunk: %w", err))
		return
	}
	if err := os.Chmod(path, 0o600); err != nil {
		r.setErr(fmt.Errorf("protect LLM response chunk: %w", err))
		return
	}
	timestamp := time.Now().UTC()
	r.appendEvent(domain.AgentRawEvent{
		Type:        domain.AgentRawEventLLMChunk,
		Timestamp:   &timestamp,
		Method:      req.Method,
		Path:        req.URL.EscapedPath(),
		ChunkRef:    runRelativeArtifactPath(r.artifactDir, path),
		RequestRef:  RequestRef(requestID),
		ResponseRef: ResponseRef(responseID),
	})
}

// OpenResponseBody creates a file for streaming the response body.
func (r *Recorder) OpenResponseBody(responseID int) (*os.File, string) {
	if r == nil {
		return nil, ""
	}
	llmDir, ok := r.ensureLLMDir()
	if !ok {
		return nil, ""
	}
	path := filepath.Join(llmDir, fmt.Sprintf("response-%06d.body", responseID))
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		r.setErr(fmt.Errorf("create LLM response body: %w", err))
		return nil, ""
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		r.setErr(fmt.Errorf("protect LLM response body: %w", err))
		return nil, ""
	}
	return file, runRelativeArtifactPath(r.artifactDir, path)
}

// RecordResponseDone captures the final response metadata including usage.
func (r *Recorder) RecordResponseDone(req *http.Request, resp *http.Response, requestID int, responseID int, startedAt time.Time, bodyRef string, usage *domain.UsageMetrics) {
	if r == nil || req == nil || resp == nil {
		return
	}
	if usage != nil {
		r.addUsage(*usage)
	}
	timestamp := time.Now().UTC()
	r.appendEvent(domain.AgentRawEvent{
		Type:        domain.AgentRawEventLLMDone,
		Timestamp:   &timestamp,
		Method:      req.Method,
		Path:        req.URL.EscapedPath(),
		Status:      resp.StatusCode,
		LatencyMS:   time.Since(startedAt).Milliseconds(),
		BodyRef:     bodyRef,
		Usage:       usage,
		RequestRef:  RequestRef(requestID),
		ResponseRef: ResponseRef(responseID),
	})
	if r.provider != nil && r.provider.IsInferenceRequest(req.Method, req.URL.Path) {
		r.mu.Lock()
		r.responses++
		r.mu.Unlock()
	}
}

// RecordError captures a proxy or transport error.
func (r *Recorder) RecordError(eventType string, method string, path string, err error) {
	if r == nil || err == nil {
		return
	}
	timestamp := time.Now().UTC()
	r.appendEvent(domain.AgentRawEvent{
		Type:      domain.AgentRawEventType(eventType),
		Timestamp: &timestamp,
		Method:    method,
		Path:      path,
		Error:     err.Error(),
	})
	if r.provider != nil && r.provider.IsInferenceRequest(method, path) {
		r.mu.Lock()
		r.errors++
		r.mu.Unlock()
	}
}

// AppendEvent writes an arbitrary raw event to the log.
func (r *Recorder) AppendEvent(event domain.AgentRawEvent) {
	r.appendEvent(event)
}

// Result returns aggregated telemetry from the proxy session.
func (r *Recorder) Result() (RecorderResult, error) {
	if r == nil {
		return RecorderResult{}, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	producerName := "proxy"
	if r.provider != nil {
		producerName = r.provider.Name() + "-proxy"
	}
	result := RecorderResult{
		RawLogRef:     runRelativeArtifactPath(r.artifactDir, r.rawLogPath),
		RequestCount:  r.requests,
		ResponseCount: r.responses,
		ErrorCount:    r.errors,
		Artifacts: []domain.ArtifactRef{
			{
				Path:        runRelativeArtifactPath(r.artifactDir, r.rawLogPath),
				Kind:        domain.ArtifactKindAgentRawLog,
				Description: "agent raw event log",
				Producer:    producerName,
				Role:        domain.ArtifactRoleRaw,
				MediaType:   "application/x-ndjson",
			},
		},
	}
	if r.llmDir != "" {
		result.LLMDirRef = runRelativeArtifactPath(r.artifactDir, r.llmDir)
		result.Artifacts = append(result.Artifacts, domain.ArtifactRef{
			Path:        result.LLMDirRef,
			Kind:        domain.ArtifactKindLLMTelemetry,
			Description: "LLM request and response telemetry bodies",
			Producer:    producerName,
			Role:        domain.ArtifactRoleRaw,
		})
	}
	if r.usage.InputTokens != 0 || r.usage.OutputTokens != 0 || r.usage.TotalTokens != 0 || r.usage.ToolCalls != 0 {
		usage := r.usage
		result.Usage = &usage
	}
	return result, r.firstErr
}

// NextResponse allocates the next response ID.
func (r *Recorder) NextResponse() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextResponseID++
	return r.nextResponseID
}

// ArtifactDir returns the recorder's artifact directory path.
func (r *Recorder) ArtifactDir() string {
	if r == nil {
		return ""
	}
	return r.artifactDir
}

// Provider returns the recorder's provider.
func (r *Recorder) Provider() Provider {
	if r == nil {
		return nil
	}
	return r.provider
}

// SetProvider sets the provider used for model extraction and inference
// detection. This allows the rollout layer to create a recorder early
// for command lifecycle events and upgrade it with a provider when an
// LLM proxy is set up.
func (r *Recorder) SetProvider(p Provider) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.provider = p
}

// RequestRef formats a request ID as a reference string.
func RequestRef(id int) string {
	if id <= 0 {
		return ""
	}
	return fmt.Sprintf("request-%06d", id)
}

// ResponseRef formats a response ID as a reference string.
func ResponseRef(id int) string {
	if id <= 0 {
		return ""
	}
	return fmt.Sprintf("response-%06d", id)
}

func (r *Recorder) nextRequest() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextRequestID++
	return r.nextRequestID
}

func (r *Recorder) ensureLLMDir() (string, bool) {
	if r == nil {
		return "", false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.llmDir != "" {
		return r.llmDir, true
	}
	llmDir := filepath.Join(r.artifactDir, llmArtifactDirname)
	if err := os.MkdirAll(llmDir, 0o700); err != nil {
		if r.firstErr == nil {
			r.firstErr = fmt.Errorf("create LLM telemetry artifact directory: %w", err)
		}
		return "", false
	}
	if err := os.Chmod(llmDir, 0o700); err != nil {
		if r.firstErr == nil {
			r.firstErr = fmt.Errorf("protect LLM telemetry artifact directory: %w", err)
		}
		return "", false
	}
	r.llmDir = llmDir
	return r.llmDir, true
}

func (r *Recorder) appendEvent(event domain.AgentRawEvent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	event = sanitizeEvent(event)
	if event.EventID == "" {
		r.nextEventID++
		event.EventID = fmt.Sprintf("raw-%06d", r.nextEventID)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		if r.firstErr == nil {
			r.firstErr = fmt.Errorf("marshal telemetry event: %w", err)
		}
		return
	}
	file, err := os.OpenFile(r.rawLogPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		if r.firstErr == nil {
			r.firstErr = fmt.Errorf("open agent raw log: %w", err)
		}
		return
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		if r.firstErr == nil {
			r.firstErr = fmt.Errorf("protect agent raw log: %w", err)
		}
		return
	}
	defer func() {
		if err := file.Close(); err != nil && r.firstErr == nil {
			r.firstErr = fmt.Errorf("close agent raw log: %w", err)
		}
	}()
	if _, err := file.Write(append(payload, '\n')); err != nil && r.firstErr == nil {
		r.firstErr = fmt.Errorf("write agent raw log: %w", err)
	}
}

func sanitizeEvent(event domain.AgentRawEvent) domain.AgentRawEvent {
	event.Headers = redact.Header(event.Headers)
	event.Path = redact.String(event.Path)
	event.Error = redact.String(event.Error)
	event.Command = redact.Command(event.Command)
	event.CommandText = redact.String(event.CommandText)
	return event
}

func (r *Recorder) setErr(err error) {
	if r == nil || err == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.firstErr == nil {
		r.firstErr = err
	}
}

func (r *Recorder) addUsage(usage domain.UsageMetrics) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.usage.InputTokens += usage.InputTokens
	r.usage.OutputTokens += usage.OutputTokens
	r.usage.TotalTokens += usage.TotalTokens
	r.usage.ToolCalls += usage.ToolCalls
}

func runRelativeArtifactPath(artifactDir string, path string) string {
	return runref.ArtifactPath(artifactDir, path)
}

// ResponseBodyPath resolves a run-relative body ref to an absolute path.
func ResponseBodyPath(artifactDir string, ref string) string {
	if ref == "" {
		return ""
	}
	return filepath.Join(runref.RunDirFromArtifactDir(artifactDir), filepath.FromSlash(ref))
}
