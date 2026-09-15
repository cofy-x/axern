package app

import (
	httpapi "github.com/cofy-x/axern/gateway/gatewayd/internal/api/http"
	nodeapi "github.com/cofy-x/axern/gateway/gatewayd/internal/api/node"
	term "github.com/cofy-x/axern/gateway/gatewayd/internal/application/terminal"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/config"
)

func terminalOptions(cfg config.Config) term.Options {
	return term.Options{
		IdleTimeout:              cfg.TerminalIdleTimeout,
		MaxDuration:              cfg.TerminalMaxDuration,
		AccessGrantRetryAttempts: cfg.AccessGrantRetryAttempts,
		AccessGrantRetryDelay:    cfg.AccessGrantRetryBaseDelay,
	}
}

func nodeOptions(cfg config.Config) nodeapi.Options {
	return nodeapi.Options{
		AccessGrantRetryAttempts: cfg.AccessGrantRetryAttempts,
		AccessGrantRetryDelay:    cfg.AccessGrantRetryBaseDelay,
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
