package wire

import (
	"regexp"
	"time"
)

const MaxBackgroundProcessSeconds = 3600
const PathBackgroundProcesses = "/background-processes"

var backgroundProcessRef = regexp.MustCompile(`^[A-Za-z0-9_.-]{0,128}$`)

func ValidBackgroundProcessRef(value string) bool { return backgroundProcessRef.MatchString(value) }

type BackgroundProcessSpec struct {
	Argv      []string
	Env       map[string]string
	Cwd       string
	User      string
	CallerRef string
	Timeout   time.Duration
}

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
	ID              string     `json:"id"`
	State           string     `json:"state"`
	PID             int        `json:"pid,omitempty"`
	ExitCode        *int       `json:"exitCode,omitempty"`
	Signal          string     `json:"signal,omitempty"`
	StartedAt       *time.Time `json:"startedAt,omitempty"`
	FinishedAt      *time.Time `json:"finishedAt,omitempty"`
	LastError       string     `json:"lastError,omitempty"`
	Stdout          string     `json:"stdout,omitempty"`
	Stderr          string     `json:"stderr,omitempty"`
	StdoutTruncated bool       `json:"stdoutTruncated,omitempty"`
	StderrTruncated bool       `json:"stderrTruncated,omitempty"`
	Background      bool       `json:"background,omitempty"`
	CallerRef       string     `json:"callerRef,omitempty"`
}
