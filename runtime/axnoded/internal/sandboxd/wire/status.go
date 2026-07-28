package wire

import "time"

type HealthResponse struct {
	ProtocolVersion int    `json:"protocolVersion,omitempty"`
	Status          string `json:"status"`
}

type ReadyResponse struct {
	ProtocolVersion int  `json:"protocolVersion,omitempty"`
	Ready           bool `json:"ready"`
}

type CapabilitiesResponse struct {
	ProtocolVersion int                  `json:"protocolVersion,omitempty"`
	Capabilities    []string             `json:"capabilities"`
	Providers       []CapabilityProvider `json:"providers,omitempty"`
	Summary         ProviderSummary      `json:"summary,omitempty"`
}

type CapabilityProvider struct {
	Name         string               `json:"name"`
	State        string               `json:"state,omitempty"`
	Available    bool                 `json:"available"`
	Capabilities []string             `json:"capabilities,omitempty"`
	Backend      string               `json:"backend,omitempty"`
	Command      string               `json:"command,omitempty"`
	Reason       string               `json:"reason,omitempty"`
	LastError    string               `json:"lastError,omitempty"`
	Dependencies []ProviderDependency `json:"dependencies,omitempty"`
}

type ProviderDependency struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type ProviderSummary struct {
	Total       int `json:"total,omitempty"`
	Available   int `json:"available,omitempty"`
	Degraded    int `json:"degraded,omitempty"`
	Unavailable int `json:"unavailable,omitempty"`
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
	ProtocolVersion int               `json:"protocolVersion,omitempty"`
	DaemonPID       int               `json:"daemonPid"`
	UptimeSeconds   float64           `json:"uptimeSeconds"`
	SocketPath      string            `json:"socketPath"`
	UserProcess     UserProcessStatus `json:"userProcess"`
}

type ReadySnapshot struct {
	Ready        ReadyResponse
	Status       StatusResponse
	Capabilities CapabilitiesResponse
}

type DiagnosticsResponse struct {
	ProtocolVersion int                        `json:"protocolVersion,omitempty"`
	GeneratedAt     time.Time                  `json:"generatedAt"`
	Ready           bool                       `json:"ready"`
	Detail          string                     `json:"detail,omitempty"`
	Status          StatusResponse             `json:"status"`
	Capabilities    []string                   `json:"capabilities"`
	Providers       []CapabilityProvider       `json:"providers,omitempty"`
	ProviderSummary ProviderSummary            `json:"providerSummary,omitempty"`
	ProcessSummary  ProcessSummary             `json:"processSummary,omitempty"`
	FileLimits      *FileLimitSnapshot         `json:"fileLimits,omitempty"`
	Processes       *ProcessListResponse       `json:"processes,omitempty"`
	Ports           *PortSnapshot              `json:"ports,omitempty"`
	Mounts          *MountSnapshot             `json:"mounts,omitempty"`
	ComputerUse     *ComputerUseStatusResponse `json:"computerUse,omitempty"`
	Browser         *BrowserStatusResponse     `json:"browser,omitempty"`
}
