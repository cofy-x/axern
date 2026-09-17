package wire

import "time"

type WorkloadStopRequest struct {
	Signal string `json:"signal"`
}

type WorkloadExitResponse struct {
	ExitCode  int       `json:"exitCode"`
	Signal    string    `json:"signal,omitempty"`
	ExitedAt  time.Time `json:"exitedAt"`
	LastError string    `json:"lastError,omitempty"`
}
