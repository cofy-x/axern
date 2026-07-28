package config

import "time"

const (
	DefaultHTTPAddress        = "127.0.0.1:25080"
	DefaultSSHAddress         = "127.0.0.1:25022"
	DefaultControlEdgeAddress = "127.0.0.1:25000"
	DefaultControlTarget      = "127.0.0.1:24000"
	DefaultTunnelRelayTarget  = "127.0.0.1:24100"
	DefaultTLSCACert          = ".dev/certs/ca.crt"
	DefaultTLSCert            = ".dev/certs/gatewayd.crt"
	DefaultTLSKey             = ".dev/certs/gatewayd.key"
	DefaultControlEdgeTLSCert = ".dev/certs/gatewayd.crt"
	DefaultControlEdgeTLSKey  = ".dev/certs/gatewayd.key"
	DefaultDashboardVendorDir = "/usr/local/share/axern/gatewayd/dashboard/vendor"
)

type Config struct {
	HTTPAddress                  string
	ControlEdgeAddress           string
	ControlEdgeTLSCACert         string
	ControlEdgeTLSCert           string
	ControlEdgeTLSKey            string
	TunnelRelayTarget            string
	TunnelRelayTLSCACert         string
	TunnelRelayTLSServerName     string
	ControlTarget                string
	TLSCACert                    string
	TLSCert                      string
	TLSKey                       string
	DevToken                     string
	RequireHTTPAuth              bool
	SSHEnabled                   bool
	SSHAddress                   string
	SSHHostKey                   string
	SSHAuthorizedKeys            string
	DashboardEnabled             bool
	DashboardVendorDir           string
	RouteCacheTTL                time.Duration
	RouteCacheMaxEntries         int
	ControlDialTimeout           time.Duration
	ReadHeaderTimeout            time.Duration
	ReadTimeout                  time.Duration
	WriteTimeout                 time.Duration
	IdleTimeout                  time.Duration
	ServiceUpstreamTimeout       time.Duration
	ServiceMaxRequestBodyBytes   int64
	ServiceEndpointRetryAttempts int
	ServiceEndpointQuarantineTTL time.Duration
	TerminalIdleTimeout          time.Duration
	TerminalMaxDuration          time.Duration
	TerminalMaxMessageBytes      int64
	LeaseRetryAttempts           int
	LeaseRetryBaseDelay          time.Duration
	ArtifactMaxConcurrent        int
	ArtifactChunkBytes           int
	ArtifactUpstreamTimeout      time.Duration
	ArtifactMaxBytes             int64
	LogLevel                     string
}

func Parse(args []string) (Config, error) {
	cfg := defaultsFromEnv()
	flags := newFlagSet(&cfg)
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	return validate(cfg)
}
