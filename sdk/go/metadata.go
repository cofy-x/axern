package axernsdk

import "time"

// SandboxMetadata is a stable diagnostic view of a started sandbox.
type SandboxMetadata struct {
	EnvironmentID   string
	RunID           string
	AllocationID    string
	StartedAt       time.Time
	TunnelSessionID string
	BoundAddr       string
	Labels          map[string]string
}

// Metadata returns diagnostic metadata for a started sandbox.
func (s *Sandbox) Metadata() (SandboxMetadata, error) {
	if !s.started {
		return SandboxMetadata{}, ErrSandboxNotStarted
	}
	return SandboxMetadata{
		EnvironmentID:   s.state.EnvironmentID,
		RunID:           s.state.RunID,
		AllocationID:    s.state.AllocationID,
		StartedAt:       s.state.StartedAt,
		TunnelSessionID: s.state.TunnelSessionID,
		BoundAddr:       s.state.BoundAddr,
		Labels:          cloneMap(s.options.Labels),
	}, nil
}
