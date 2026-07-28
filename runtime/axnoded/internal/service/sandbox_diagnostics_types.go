package service

import "time"

type SandboxCapabilityStatus struct {
	Ready           bool
	Capabilities    []string
	Providers       []SandboxCapabilityProvider
	ProviderSummary SandboxCapabilityProviderSummary
}

type SandboxCapabilityProvider struct {
	Name         string
	State        string
	Available    bool
	Capabilities []string
	Backend      string
	Reason       string
	Dependencies []SandboxCapabilityDependency
}

type SandboxCapabilityDependency struct {
	Name      string
	Available bool
	Reason    string
}

type SandboxCapabilityProviderSummary struct {
	Total       int
	Available   int
	Degraded    int
	Unavailable int
}

type SandboxdDiagnostics struct {
	GeneratedAt     time.Time
	Ready           bool
	Detail          string
	Status          SandboxdDiagnosticsStatus
	Capabilities    []string
	Providers       []SandboxdProvider
	ProviderSummary SandboxdProviderSummary
	ProcessSummary  SandboxdProcessSummary
	RawJSON         string
}

type SandboxdDiagnosticsStatus struct {
	DaemonPID     int
	UptimeSeconds float64
	SocketPath    string
	UserState     string
}

type SandboxdProvider struct {
	Name         string
	State        string
	Available    bool
	Capabilities []string
	Backend      string
	Command      string
	Reason       string
	LastError    string
	Dependencies []SandboxdProviderDependency
}

type SandboxdProviderDependency struct {
	Name      string
	Available bool
	Reason    string
}

type SandboxdProviderSummary struct {
	Total       int
	Available   int
	Degraded    int
	Unavailable int
}

type SandboxdProcessSummary struct {
	Total    int
	Starting int
	Running  int
	Exited   int
	Failed   int
}
