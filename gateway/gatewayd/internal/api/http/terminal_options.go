package httpapi

import "time"

type TerminalOptions struct {
	IdleTimeout     time.Duration
	MaxDuration     time.Duration
	MaxMessageBytes int64
	WriteTimeout    time.Duration
}
