package axernsdk

import "time"

// SandboxMetadata is a stable diagnostic view of a started sandbox.
type SandboxMetadata struct {
	EnvironmentID   string
	ServiceID       string
	AllocationID    string
	Attempt         int64
	NodeID          string
	RuntimeClass    string
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
		ServiceID:       s.state.ServiceID,
		AllocationID:    s.state.AllocationID,
		Attempt:         s.state.Attempt,
		NodeID:          s.state.NodeID,
		RuntimeClass:    s.options.RuntimeClass,
		StartedAt:       s.state.StartedAt,
		TunnelSessionID: s.state.TunnelSessionID,
		BoundAddr:       s.state.BoundAddr,
		Labels:          cloneMap(s.options.Labels),
	}, nil
}
