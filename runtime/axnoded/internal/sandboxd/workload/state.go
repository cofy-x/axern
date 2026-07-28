package workload

import (
	"os"
	"sync"
	"time"
)

const (
	UserStateNotStarted = "not_started"
	UserStateStarting   = "starting"
	UserStateRunning    = "running"
	UserStateExited     = "exited"
	UserStateFailed     = "failed"
	UserStateStopping   = "stopping"
)

type State struct {
	startedAt  time.Time
	socketPath string

	mu          sync.RWMutex
	userProcess UserProcessStatus
}

type UserProcessStatus struct {
	State      string     `json:"state"`
	PID        int        `json:"pid,omitempty"`
	ExitCode   *int       `json:"exitCode,omitempty"`
	Signal     string     `json:"signal,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	LastError  string     `json:"lastError,omitempty"`
}

type StatusResponse struct {
	DaemonPID     int               `json:"daemonPid"`
	UptimeSeconds float64           `json:"uptimeSeconds"`
	SocketPath    string            `json:"socketPath"`
	UserProcess   UserProcessStatus `json:"userProcess"`
}

func NewState(socketPath string) *State {
	return &State{
		startedAt:  time.Now().UTC(),
		socketPath: socketPath,
		userProcess: UserProcessStatus{
			State: UserStateNotStarted,
		},
	}
}

func (s *State) Status() StatusResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return StatusResponse{
		DaemonPID:     os.Getpid(),
		UptimeSeconds: time.Since(s.startedAt).Seconds(),
		SocketPath:    s.socketPath,
		UserProcess:   s.userProcess,
	}
}

func (s *State) SetUserProcess(update UserProcessStatus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.userProcess = update
}

func (s *State) UpdateUserProcess(fn func(*UserProcessStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.userProcess)
}
