package localruntime

import (
	"context"
	"time"
)

const (
	ProjectName        = "axern-local"
	ContextName        = "local"
	LocalNodeID        = "node-local"
	GatewayControlPort = 25000
	GatewayHTTPPort    = 25080
	GatewaySSHPort     = 25022
)

type Metadata struct {
	Version   string    `json:"version"`
	Profile   string    `json:"profile,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Component struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Health string `json:"health,omitempty"`
}

type Status struct {
	State          string         `json:"state"`
	CLIVersion     string         `json:"cli_version"`
	StackVersion   string         `json:"stack_version,omitempty"`
	Profile        string         `json:"profile,omitempty"`
	DataPath       string         `json:"data_path"`
	DiskBytes      int64          `json:"disk_bytes"`
	DashboardURL   string         `json:"dashboard_url"`
	GatewayTarget  string         `json:"gateway_target"`
	Ports          map[string]int `json:"ports"`
	CurrentContext string         `json:"current_context,omitempty"`
	ContextCurrent bool           `json:"context_current"`
	Components     []Component    `json:"components,omitempty"`
}

type Check struct {
	Name        string           `json:"name"`
	Status      string           `json:"status"`
	Code        string           `json:"code"`
	DurationMS  int64            `json:"duration_ms"`
	Message     string           `json:"message"`
	Remediation string           `json:"remediation,omitempty"`
	Details     map[string]int64 `json:"details,omitempty"`
}

type DoctorReport struct {
	Status string  `json:"status"`
	Mode   string  `json:"mode"`
	Checks []Check `json:"checks"`
}

type DoctorOptions struct {
	Probe        bool
	QueryName    string
	CheckTimeout time.Duration
	SandboxProbe func(context.Context) Check
}

type UpOptions struct {
	Profile          string
	Use              bool
	ReadinessTimeout time.Duration
}

type LogOptions struct {
	Component string
	Follow    bool
	Tail      int
	Since     string
}
