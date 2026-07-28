package llmproxy

import "time"

type Report struct {
	Provider      string        `json:"provider,omitempty"`
	RequestCount  int           `json:"requestCount,omitempty"`
	ResponseCount int           `json:"responseCount,omitempty"`
	ErrorCount    int           `json:"errorCount,omitempty"`
	Usage         *Usage        `json:"usage,omitempty"`
	Events        []Event       `json:"events,omitempty"`
	Bodies        []BodyCapture `json:"bodies,omitempty"`
	Truncated     bool          `json:"truncated,omitempty"`
	DroppedEvents int           `json:"droppedEvents,omitempty"`
	DroppedBodies int           `json:"droppedBodies,omitempty"`
	DroppedBytes  int64         `json:"droppedBytes,omitempty"`
}

type Event struct {
	Type          string              `json:"type"`
	Timestamp     time.Time           `json:"timestamp"`
	Method        string              `json:"method,omitempty"`
	Path          string              `json:"path,omitempty"`
	Status        int                 `json:"status,omitempty"`
	Model         string              `json:"model,omitempty"`
	Headers       map[string][]string `json:"headers,omitempty"`
	BodyRef       string              `json:"bodyRef,omitempty"`
	ChunkRef      string              `json:"chunkRef,omitempty"`
	RequestRef    string              `json:"requestRef,omitempty"`
	ResponseRef   string              `json:"responseRef,omitempty"`
	LatencyMS     int64               `json:"latencyMs,omitempty"`
	Usage         *Usage              `json:"usage,omitempty"`
	Error         string              `json:"error,omitempty"`
	DroppedEvents int                 `json:"droppedEvents,omitempty"`
	DroppedBodies int                 `json:"droppedBodies,omitempty"`
	DroppedBytes  int64               `json:"droppedBytes,omitempty"`
}

type BodyCapture struct {
	Ref          string `json:"ref"`
	Data         []byte `json:"data,omitempty"`
	OriginalSize int64  `json:"originalSize,omitempty"`
	Truncated    bool   `json:"truncated,omitempty"`
}
