package wire

import "time"

type ProcessListResponse struct {
	Processes []ProcessStatus `json:"processes"`
}

type ProcessSummary struct {
	Total    int `json:"total,omitempty"`
	Starting int `json:"starting,omitempty"`
	Running  int `json:"running,omitempty"`
	Exited   int `json:"exited,omitempty"`
	Failed   int `json:"failed,omitempty"`
}

type ProcessStatus struct {
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

type ManagedProxyReport struct {
	Provider      string `json:"provider,omitempty"`
	RequestCount  int    `json:"requestCount,omitempty"`
	ResponseCount int    `json:"responseCount,omitempty"`
	ErrorCount    int    `json:"errorCount,omitempty"`
	ReportJSON    []byte `json:"reportJson,omitempty"`
}
