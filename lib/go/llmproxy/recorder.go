package llmproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	EventLLMRequest   = "llm.request"
	EventLLMResponse  = "llm.response"
	EventLLMChunk     = "llm.response_chunk"
	EventLLMDone      = "llm.response_done"
	EventLLMError     = "llm.error"
	EventLLMTruncated = "llm.telemetry_truncated"

	maxCapturedEvents    = 4096
	maxCapturedBodyBytes = 1 << 20
)

type Recorder struct {
	provider Provider

	mu             sync.Mutex
	nextRequestID  int
	nextResponseID int
	requests       int
	responses      int
	errors         int
	usage          Usage
	events         []Event
	bodies         []BodyCapture
	capturedBytes  int64
	droppedEvents  int
	droppedBodies  int
	droppedBytes   int64
}

func NewRecorder(provider Provider) *Recorder {
	return &Recorder{provider: provider}
}

func (r *Recorder) RecordRequest(req *http.Request) int {
	if r == nil || req == nil {
		return 0
	}
	body, err := readAndRestoreBody(req)
	if err != nil {
		r.RecordError(req.Method, req.URL.Path, err)
		return 0
	}
	requestID := r.nextRequest()
	requestRef := requestRef(requestID)
	bodyRef := ""
	if len(body) > 0 {
		bodyRef = r.appendBody(fmt.Sprintf("request-%06d.body", requestID), body)
	}
	model := ""
	if r.provider != nil {
		model = r.provider.ExtractModel(body)
	}
	r.appendEvent(Event{
		Type:       EventLLMRequest,
		Timestamp:  time.Now().UTC(),
		Method:     req.Method,
		Path:       req.URL.EscapedPath(),
		Model:      model,
		Headers:    SanitizeHeaders(req.Header),
		BodyRef:    bodyRef,
		RequestRef: requestRef,
	})
	if r.provider != nil && r.provider.IsInferenceRequest(req.Method, req.URL.Path) {
		r.mu.Lock()
		r.requests++
		r.mu.Unlock()
	}
	return requestID
}

func (r *Recorder) RecordResponseStart(req *http.Request, resp *http.Response, requestID int, responseID int) {
	if r == nil || req == nil || resp == nil {
		return
	}
	r.appendEvent(Event{
		Type:        EventLLMResponse,
		Timestamp:   time.Now().UTC(),
		Method:      req.Method,
		Path:        req.URL.EscapedPath(),
		Status:      resp.StatusCode,
		Headers:     SanitizeHeaders(resp.Header),
		RequestRef:  requestRef(requestID),
		ResponseRef: responseRef(responseID),
	})
}

func (r *Recorder) RecordResponseChunk(req *http.Request, requestID int, responseID int, chunkIndex int, chunk []byte) {
	if r == nil || req == nil || len(chunk) == 0 {
		return
	}
	chunkRef := fmt.Sprintf("response-%06d-chunk-%06d.body", responseID, chunkIndex)
	chunkRef = r.appendBody(chunkRef, chunk)
	r.appendEvent(Event{
		Type:        EventLLMChunk,
		Timestamp:   time.Now().UTC(),
		Method:      req.Method,
		Path:        req.URL.EscapedPath(),
		ChunkRef:    chunkRef,
		RequestRef:  requestRef(requestID),
		ResponseRef: responseRef(responseID),
	})
}

func (r *Recorder) RecordResponseDone(req *http.Request, resp *http.Response, requestID int, responseID int, startedAt time.Time, body []byte, bodySize int64, usage *Usage) {
	if r == nil || req == nil || resp == nil {
		return
	}
	bodyRef := ""
	if len(body) > 0 {
		bodyRef = r.appendBodyWithSize(fmt.Sprintf("response-%06d.body", responseID), body, bodySize)
	}
	if usage == nil && r.provider != nil {
		usage = r.provider.ExtractUsage(body)
	}
	if usage != nil {
		r.addUsage(*usage)
	}
	r.appendEvent(Event{
		Type:        EventLLMDone,
		Timestamp:   time.Now().UTC(),
		Method:      req.Method,
		Path:        req.URL.EscapedPath(),
		Status:      resp.StatusCode,
		LatencyMS:   time.Since(startedAt).Milliseconds(),
		BodyRef:     bodyRef,
		Usage:       usage,
		RequestRef:  requestRef(requestID),
		ResponseRef: responseRef(responseID),
	})
	if r.provider != nil && r.provider.IsInferenceRequest(req.Method, req.URL.Path) {
		r.mu.Lock()
		r.responses++
		r.mu.Unlock()
	}
}

func (r *Recorder) RecordError(method string, path string, err error) {
	if r == nil || err == nil {
		return
	}
	r.appendEvent(Event{
		Type:      EventLLMError,
		Timestamp: time.Now().UTC(),
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

func (r *Recorder) NextResponse() int {
	if r == nil {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextResponseID++
	return r.nextResponseID
}

func (r *Recorder) Report() Report {
	if r == nil {
		return Report{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	providerName := ""
	if r.provider != nil {
		providerName = r.provider.Name()
	}
	var usage *Usage
	if r.usage.InputTokens != 0 || r.usage.OutputTokens != 0 || r.usage.CacheReadTokens != 0 || r.usage.TotalTokens != 0 {
		copied := r.usage
		usage = &copied
	}
	return Report{
		Provider:      providerName,
		RequestCount:  r.requests,
		ResponseCount: r.responses,
		ErrorCount:    r.errors,
		Usage:         usage,
		Events:        append([]Event(nil), r.events...),
		Bodies:        append([]BodyCapture(nil), r.bodies...),
		Truncated:     r.droppedEvents > 0 || r.droppedBodies > 0 || r.droppedBytes > 0,
		DroppedEvents: r.droppedEvents,
		DroppedBodies: r.droppedBodies,
		DroppedBytes:  r.droppedBytes,
	}
}

// TransportReport returns a report that is safe to embed in a unary process
// result. Response bodies and streaming chunk events are local capture details;
// aggregate usage and lifecycle events are the durable execution contract.
func (r *Recorder) TransportReport() Report {
	report := r.Report()
	report.DroppedBodies += len(report.Bodies)
	for _, body := range report.Bodies {
		report.DroppedBytes += int64(len(body.Data))
	}
	report.Bodies = nil
	events := make([]Event, 0, len(report.Events))
	for _, event := range report.Events {
		if event.Type == EventLLMChunk {
			report.DroppedEvents++
			continue
		}
		event.BodyRef = ""
		event.ChunkRef = ""
		events = append(events, event)
	}
	report.Events = events
	report.Truncated = report.DroppedEvents > 0 || report.DroppedBodies > 0 || report.DroppedBytes > 0
	if report.Truncated {
		report.Events = append(report.Events, truncationEvent(report))
	}
	return report
}

// MarshalTransportReport enforces an encoded-size ceiling for the unary
// process result. Aggregate counts and usage are always retained; lifecycle
// events are discarded as a unit if unusually large metadata exceeds the
// ceiling.
func (r *Recorder) MarshalTransportReport(maxBytes int) (Report, []byte, error) {
	if maxBytes <= 0 {
		return Report{}, nil, fmt.Errorf("managed proxy report limit must be positive")
	}
	report := r.TransportReport()
	payload, err := json.Marshal(report)
	if err != nil {
		return Report{}, nil, err
	}
	if len(payload) <= maxBytes {
		return report, payload, nil
	}
	report.DroppedEvents += len(report.Events)
	report.Events = nil
	report.Truncated = true
	report.Events = append(report.Events, truncationEvent(report))
	payload, err = json.Marshal(report)
	if err != nil {
		return Report{}, nil, err
	}
	if len(payload) > maxBytes {
		return Report{}, nil, fmt.Errorf("managed proxy report metadata exceeds %d bytes", maxBytes)
	}
	return report, payload, nil
}

func truncationEvent(report Report) Event {
	return Event{
		Type:          EventLLMTruncated,
		Timestamp:     time.Now().UTC(),
		DroppedEvents: report.DroppedEvents,
		DroppedBodies: report.DroppedBodies,
		DroppedBytes:  report.DroppedBytes,
		Error: fmt.Sprintf(
			"managed proxy telemetry bounded: dropped_events=%d dropped_bodies=%d dropped_bytes=%d",
			report.DroppedEvents,
			report.DroppedBodies,
			report.DroppedBytes,
		),
	}
}

func (r *Recorder) nextRequest() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextRequestID++
	return r.nextRequestID
}

func (r *Recorder) appendEvent(event Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) >= maxCapturedEvents {
		r.droppedEvents++
		return
	}
	r.events = append(r.events, event)
}

func (r *Recorder) appendBody(ref string, body []byte) string {
	return r.appendBodyWithSize(ref, body, int64(len(body)))
}

func (r *Recorder) appendBodyWithSize(ref string, body []byte, originalSize int64) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if originalSize < int64(len(body)) {
		originalSize = int64(len(body))
	}
	remaining := int64(maxCapturedBodyBytes) - r.capturedBytes
	if remaining <= 0 {
		r.droppedBodies++
		r.droppedBytes += originalSize
		return ""
	}
	captureSize := int64(len(body))
	if captureSize > remaining {
		captureSize = remaining
	}
	captured := append([]byte(nil), body[:int(captureSize)]...)
	entry := BodyCapture{Ref: ref, Data: captured, OriginalSize: originalSize}
	if captureSize < originalSize {
		entry.Truncated = true
		r.droppedBodies++
		r.droppedBytes += originalSize - captureSize
	}
	r.bodies = append(r.bodies, entry)
	r.capturedBytes += captureSize
	return ref
}

func (r *Recorder) addUsage(usage Usage) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.usage.InputTokens += usage.InputTokens
	r.usage.OutputTokens += usage.OutputTokens
	r.usage.CacheReadTokens += usage.CacheReadTokens
	r.usage.TotalTokens += usage.TotalTokens
}

func readAndRestoreBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	if err := req.Body.Close(); err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

func requestRef(id int) string {
	if id <= 0 {
		return ""
	}
	return fmt.Sprintf("req-%06d", id)
}

func responseRef(id int) string {
	if id <= 0 {
		return ""
	}
	return fmt.Sprintf("resp-%06d", id)
}
