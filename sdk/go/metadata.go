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
	state, err := s.State()
	if err != nil {
		return SandboxMetadata{}, err
	}
	return SandboxMetadata{
		EnvironmentID:   state.EnvironmentID,
		RunID:           state.RunID,
		AllocationID:    state.AllocationID,
		StartedAt:       state.StartedAt,
		TunnelSessionID: state.TunnelSessionID,
		BoundAddr:       state.BoundAddr,
		Labels:          cloneMap(s.options.Labels),
	}, nil
}
