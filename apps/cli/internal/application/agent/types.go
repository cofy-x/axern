package agent

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	appcatalog "github.com/cofy-x/axern/apps/cli/internal/application/catalog"
	appenvironment "github.com/cofy-x/axern/apps/cli/internal/application/environment"
	appservice "github.com/cofy-x/axern/apps/cli/internal/application/service"
	apptunnel "github.com/cofy-x/axern/apps/cli/internal/application/tunnel"
	"github.com/cofy-x/axern/lib/go/agentprofile"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
)

const (
	DefaultProfileName = agentprofile.DefaultProfileName
	DefaultNamespace   = "default"
	DefaultReplicas    = int32(1)
	DefaultRemoteUser  = "axern"
	DefaultRemoteShell = "/bin/bash -l"
	DefaultWorkspace   = "/home/axern/workspace"

	LabelWorkflow  = "axern.io/workflow"
	LabelAgent     = "axern.io/agent"
	LabelProfile   = "axern.io/agent-profile"
	LabelWorkspace = "axern.io/agent-workspace"

	WorkspaceVolumePrefix  = "agent-workspace-"
	WorkspaceNameMaxLength = 48
)

type Mode string

const (
	ModeConnect Mode = "connect"
	ModeShell   Mode = "shell"
	ModeRun     Mode = "run"
)

type ServiceClient interface {
	appservice.ServiceClient
}

type ProviderProber interface {
	Probe(context.Context, agentprofile.ProbeRequest) (agentprofile.ProbeResult, error)
}

type RemoteRunner interface {
	WriteAgentConfig(ctx context.Context, target RemoteTarget, remotePort int32, profile agentprofile.Profile, localToken string) error
	RestoreAgentConfig(ctx context.Context, target RemoteTarget, agentType agentprofile.AgentType) error
	Run(ctx context.Context, target RemoteTarget, command string, requestTTY bool) error
}

type RemoteTarget struct {
	AllocationID          string
	User                  string
	SSHTarget             string
	SSHKey                string
	StrictHostKeyChecking bool
}

type Params struct {
	CreateContext  context.Context
	Profile        agentprofile.Profile
	Workspace      string
	ServiceClient  ServiceClient
	Catalog        appcatalog.RuntimeCatalogClient
	Environment    appenvironment.EnvironmentClient
	Tunnel         apptunnel.Control
	Remote         RemoteRunner
	RemoteTarget   RemoteTarget
	Mode           Mode
	RunArgs        []string
	TTL            time.Duration
	ReadyTimeout   time.Duration
	ServiceTimeout time.Duration
	Relay          apptunnel.RelayDialConfig
	RelayDialer    apptunnel.RelayPeerDialer
	Connector      apptunnel.ConnectorConfig
	OnReconnect    apptunnel.ConnectorReconnectReporter
	OnReady        func(Result) error
}

type Result struct {
	Agent             string
	ProfileName       string
	Workspace         string
	ServiceID         string
	CreatedService    bool
	AllocationID      string
	NodeID            string
	Session           *tunnelcontrolv1.TunnelSession
	RemoteBindAddress string
	Upstream          string
}

type Control struct{}

func New() Control {
	return Control{}
}

func ValidateProfile(profile agentprofile.Profile) error {
	adapter, err := AdapterFor(profile.Agent)
	if err != nil {
		return err
	}
	return adapter.Validate(profile)
}

func normalizedProfileName(name string) string {
	if strings.TrimSpace(name) == "" {
		return DefaultProfileName
	}
	return strings.TrimSpace(name)
}

func ResolveWorkspaceName(value, profileName string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" {
		name = normalizedProfileName(profileName)
	}
	if len(name) > WorkspaceNameMaxLength {
		return "", fmt.Errorf("agent workspace name must be at most %d characters", WorkspaceNameMaxLength)
	}
	for index, r := range name {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || index > 0 && (r == '.' || r == '_' || r == '-') {
			continue
		}
		return "", fmt.Errorf("agent workspace name %q must start with a lowercase letter or digit and contain only lowercase letters, digits, '.', '_', or '-'", name)
	}
	return name, nil
}

func workspaceVolumeName(workspace string) string {
	return WorkspaceVolumePrefix + workspace
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func RedactURLUser(u *url.URL) string {
	if u == nil {
		return ""
	}
	clone := *u
	if clone.User != nil {
		clone.User = url.User("***")
	}
	clone.RawQuery = ""
	return clone.String()
}

func errEnvironmentResolverRequired() error {
	return fmt.Errorf("environment resolver is required")
}
