package config

import "time"

const (
	DefaultHTTPAddress        = "127.0.0.1:25080"
	DefaultSSHAddress         = "127.0.0.1:25022"
	DefaultControlEdgeAddress = "127.0.0.1:25000"
	DefaultControlTarget      = "127.0.0.1:24000"
	DefaultTunnelRelayTarget  = "127.0.0.1:24100"
	DefaultTLSCACert          = ".dev/certs/ca.crt"
	DefaultControlEdgeTLSCert = ".dev/certs/gatewayd.pem"
	DefaultControlEdgeTLSKey  = ".dev/certs/gatewayd.pem"
)

type Config struct {
	WorkloadBundle            string
	WorkloadCluster           string
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
	SSHEnabled                bool
	SSHAddress                string
	SSHHostKey                string
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
