package localruntime

import "time"

const (
	ProjectName        = "axern-local"
	ContextName        = "local"
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
	Code           string `json:"code"`
	OK             bool   `json:"ok"`
	Severity       string `json:"severity"`
	Message        string `json:"message"`
	Recommendation string `json:"recommendation,omitempty"`
}

type DoctorReport struct {
	Healthy bool    `json:"healthy"`
	Checks  []Check `json:"checks"`
}

type UpOptions struct {
	Profile string
	Use     bool
}

type LogOptions struct {
	Component string
	Follow    bool
	Tail      int
	Since     string
}
