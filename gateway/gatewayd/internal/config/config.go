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
	DefaultNodeTLSServerName  = "axern-node"
)

type Config struct {
	HTTPAddress               string
	ControlEdgeAddress        string
	ControlEdgeTLSCACert      string
	ControlEdgeTLSCert        string
	ControlEdgeTLSKey         string
	TunnelRelayTarget         string
	TunnelRelayTLSCACert      string
	TunnelRelayTLSServerName  string
	ControlTarget             string
	TLSCACert                 string
	TLSCert                   string
	TLSKey                    string
	NodeTLSCACert             string
	NodeTLSCert               string
	NodeTLSKey                string
	NodeTLSServerName         string
	DevToken                  string
	SSHEnabled                bool
	SSHAddress                string
	SSHHostKey                string
	SSHAuthorizedKeys         string
	ControlDialTimeout        time.Duration
	ReadHeaderTimeout         time.Duration
	ReadTimeout               time.Duration
	WriteTimeout              time.Duration
	IdleTimeout               time.Duration
	TerminalIdleTimeout       time.Duration
	TerminalMaxDuration       time.Duration
	TerminalMaxMessageBytes   int64
	AccessGrantRetryAttempts  int
	AccessGrantRetryBaseDelay time.Duration
	LogLevel                  string
}

func Parse(args []string) (Config, error) {
	cfg := defaultsFromEnv()
	flags := newFlagSet(&cfg)
	if err := flags.Parse(args); err != nil {
		return Config{}, err
	}
	return validate(cfg)
}
