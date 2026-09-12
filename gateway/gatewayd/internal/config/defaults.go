package config

import (
	"os"
	"time"
)

func defaultsFromEnv() Config {
	return Config{
		HTTPAddress:              defaultString(os.Getenv("GATEWAYD_HTTP_ADDRESS"), DefaultHTTPAddress),
		ControlEdgeAddress:       defaultString(os.Getenv("GATEWAYD_CONTROL_EDGE_ADDRESS"), DefaultControlEdgeAddress),
		ControlEdgeTLSCACert:     defaultString(os.Getenv("GATEWAYD_CONTROL_EDGE_TLS_CA_CERT"), DefaultTLSCACert),
		ControlEdgeTLSCert:       defaultString(os.Getenv("GATEWAYD_CONTROL_EDGE_TLS_CERT"), DefaultControlEdgeTLSCert),
		ControlEdgeTLSKey:        defaultString(os.Getenv("GATEWAYD_CONTROL_EDGE_TLS_KEY"), DefaultControlEdgeTLSKey),
		TunnelRelayTarget:        defaultString(os.Getenv("GATEWAYD_TUNNEL_RELAY_TARGET"), DefaultTunnelRelayTarget),
		TunnelRelayTLSCACert:     defaultString(os.Getenv("GATEWAYD_TUNNEL_RELAY_TLS_CA_CERT"), DefaultTLSCACert),
		TunnelRelayTLSServerName: os.Getenv("GATEWAYD_TUNNEL_RELAY_TLS_SERVER_NAME"),
		ControlTarget:            defaultString(os.Getenv("GATEWAYD_CONTROL_TARGET"), DefaultControlTarget),
		TLSCACert:                defaultString(os.Getenv("GATEWAYD_TLS_CA_CERT"), DefaultTLSCACert),
		TLSCert:                  defaultString(os.Getenv("GATEWAYD_TLS_CERT"), DefaultTLSCert),
		TLSKey:                   defaultString(os.Getenv("GATEWAYD_TLS_KEY"), DefaultTLSKey),
		DevToken:                 os.Getenv("AXERN_GATEWAY_DEV_TOKEN"),
		SSHEnabled:               parseBool(os.Getenv("GATEWAYD_SSH_ENABLED")),
		SSHAddress:               defaultString(os.Getenv("GATEWAYD_SSH_ADDRESS"), DefaultSSHAddress),
		SSHHostKey:               os.Getenv("GATEWAYD_SSH_HOST_KEY"),
		SSHAuthorizedKeys:        os.Getenv("GATEWAYD_SSH_AUTHORIZED_KEYS"),
		ControlDialTimeout:       durationEnv("GATEWAYD_CONTROL_DIAL_TIMEOUT", 15*time.Second),
		ReadHeaderTimeout:        durationEnv("GATEWAYD_READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:              durationEnv("GATEWAYD_READ_TIMEOUT", 0),
		WriteTimeout:             durationEnv("GATEWAYD_WRITE_TIMEOUT", 0),
		IdleTimeout:              durationEnv("GATEWAYD_IDLE_TIMEOUT", 60*time.Second),
		TerminalIdleTimeout:      durationEnv("GATEWAYD_TERMINAL_IDLE_TIMEOUT", 10*time.Minute),
		TerminalMaxDuration:      durationEnv("GATEWAYD_TERMINAL_MAX_DURATION", 2*time.Hour),
		TerminalMaxMessageBytes:  int64Env("GATEWAYD_TERMINAL_MAX_MESSAGE_BYTES", 1<<20),
		LeaseRetryAttempts:       intEnv("GATEWAYD_LEASE_RETRY_ATTEMPTS", 3),
		LeaseRetryBaseDelay:      durationEnv("GATEWAYD_LEASE_RETRY_BASE_DELAY", 500*time.Millisecond),
		LogLevel:                 defaultString(os.Getenv("GATEWAYD_LOG_LEVEL"), "info"),
	}
}
