package process

import "time"

const (
	ProcessStateStarting = "starting"
	ProcessStateRunning  = "running"
	ProcessStateExited   = "exited"
	ProcessStateFailed   = "failed"
)

type StartRequest struct {
	Args          []string          `json:"args"`
	Cwd           string            `json:"cwd,omitempty"`
	Env           []string          `json:"env,omitempty"`
	User          string            `json:"user,omitempty"`
	Stdin         string            `json:"stdin,omitempty"`
	CaptureOutput bool              `json:"captureOutput,omitempty"`
	OpenStdin     bool              `json:"openStdin,omitempty"`
	StreamOutput  bool              `json:"streamOutput,omitempty"`
	Terminal      bool              `json:"terminal,omitempty"`
	InitialCols   uint32            `json:"initialCols,omitempty"`
	InitialRows   uint32            `json:"initialRows,omitempty"`
	TimeoutMs     int64             `json:"timeoutMs,omitempty"`
	ManagedProxy  *ManagedProxySpec `json:"managedProxy,omitempty"`
}

type ManagedProxySpec struct {
	Provider            string `json:"provider,omitempty"`
	UpstreamBaseURL     string `json:"upstreamBaseUrl,omitempty"`
	UpstreamBearerToken string `json:"upstreamBearerToken,omitempty"`
}

type ManagedProxyReport struct {
	Provider      string `json:"provider,omitempty"`
	RequestCount  int    `json:"requestCount,omitempty"`
	ResponseCount int    `json:"responseCount,omitempty"`
	ErrorCount    int    `json:"errorCount,omitempty"`
	ReportJSON    []byte `json:"reportJson,omitempty"`
}

type SignalRequest struct {
	Signal string `json:"signal"`
}

type ListResponse struct {
	Processes []Status `json:"processes"`
}

type Status struct {
	ID                 string              `json:"id"`
	State              string              `json:"state"`
	PID                int                 `json:"pid,omitempty"`
	ExitCode           *int                `json:"exitCode,omitempty"`
	Signal             string              `json:"signal,omitempty"`
	StartedAt          *time.Time          `json:"startedAt,omitempty"`
	FinishedAt         *time.Time          `json:"finishedAt,omitempty"`
	LastError          string              `json:"lastError,omitempty"`
	Stdout             string              `json:"stdout,omitempty"`
	Stderr             string              `json:"stderr,omitempty"`
	StdoutTruncated    bool                `json:"stdoutTruncated,omitempty"`
	StderrTruncated    bool                `json:"stderrTruncated,omitempty"`
	ManagedProxyReport *ManagedProxyReport `json:"managedProxyReport,omitempty"`
}

type StreamEvent struct {
	Stdout []byte `json:"stdout,omitempty"`
	Stderr []byte `json:"stderr,omitempty"`
}

type StdinRequest struct {
	Data []byte `json:"data,omitempty"`
}

type ResizeRequest struct {
	Cols uint32 `json:"cols,omitempty"`
	Rows uint32 `json:"rows,omitempty"`
}
