package sandbox

import (
	"errors"
	"fmt"

	axernsdk "github.com/cofy-x/axern/sdk/go"
)

// SandboxDeathError indicates the sandbox is permanently unreachable.
type SandboxDeathError struct {
	Cause  error
	Reason string
}

func (e *SandboxDeathError) Error() string {
	if e.Reason != "" {
		return fmt.Sprintf("sandbox dead: %s: %v", e.Reason, e.Cause)
	}
	return fmt.Sprintf("sandbox dead: %v", e.Cause)
}

func (e *SandboxDeathError) Unwrap() error {
	return e.Cause
}

// IsSandboxDeath reports whether err indicates a permanently dead sandbox.
func IsSandboxDeath(err error) bool {
	var deathErr *SandboxDeathError
	return errors.As(err, &deathErr)
}

// IsFatalSandboxError reports whether err from an Axern SDK call indicates
// that the sandbox is permanently unreachable (allocation gone or node down).
func IsFatalSandboxError(err error) bool {
	if err == nil {
		return false
	}
	if IsSandboxDeath(err) {
		return true
	}
	if errors.Is(err, axernsdk.ErrSandboxNotStarted) {
		return true
	}
	if axernsdk.IsNotFound(err) {
		return true
	}
	if axernsdk.IsUnavailable(err) {
		return true
	}
	return false
}

// ClassifyFatalReason returns a short human-readable reason for a fatal
// sandbox error, or empty string if err is not fatal.
func ClassifyFatalReason(err error) string {
	if err == nil {
		return ""
	}
	var deathErr *SandboxDeathError
	if errors.As(err, &deathErr) && deathErr.Reason != "" {
		return deathErr.Reason
	}
	if errors.Is(err, axernsdk.ErrSandboxNotStarted) {
		return "sandbox not started"
	}
	if axernsdk.IsNotFound(err) {
		return "allocation not found"
	}
	if axernsdk.IsUnavailable(err) {
		return "node unavailable"
	}
	return ""
}
