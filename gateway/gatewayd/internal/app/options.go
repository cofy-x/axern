package app

import (
	httpapi "github.com/cofy-x/axern/gateway/gatewayd/internal/api/http"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/api/http/serviceproxy"
	nodeapi "github.com/cofy-x/axern/gateway/gatewayd/internal/api/node"
	term "github.com/cofy-x/axern/gateway/gatewayd/internal/application/terminal"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/config"
)

func terminalOptions(cfg config.Config) term.Options {
	return term.Options{
		IdleTimeout:        cfg.TerminalIdleTimeout,
		MaxDuration:        cfg.TerminalMaxDuration,
		LeaseRetryAttempts: cfg.LeaseRetryAttempts,
		LeaseRetryDelay:    cfg.LeaseRetryBaseDelay,
	}
}

func serviceProxyOptions(cfg config.Config) serviceproxy.Options {
	return serviceproxy.Options{
		UpstreamTimeout:       cfg.ServiceUpstreamTimeout,
		MaxRequestBodyBytes:   cfg.ServiceMaxRequestBodyBytes,
		LeaseRetryBaseDelay:   cfg.LeaseRetryBaseDelay,
		EndpointRetryAttempts: cfg.ServiceEndpointRetryAttempts,
	}
}

func nodeOptions(cfg config.Config) nodeapi.Options {
	return nodeapi.Options{
		LeaseRetryAttempts: cfg.LeaseRetryAttempts,
		LeaseRetryDelay:    cfg.LeaseRetryBaseDelay,
	}
}

func httpTerminalOptions(cfg config.Config) httpapi.TerminalOptions {
	return httpapi.TerminalOptions{
		IdleTimeout:     cfg.TerminalIdleTimeout,
		MaxDuration:     cfg.TerminalMaxDuration,
		MaxMessageBytes: cfg.TerminalMaxMessageBytes,
		WriteTimeout:    cfg.WriteTimeout,
	}
}
