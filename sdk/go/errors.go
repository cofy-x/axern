package axernsdk

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var (
	// ErrSandboxNotStarted indicates a sandbox runtime API was called before Start.
	ErrSandboxNotStarted = errors.New("sandbox is not active")
	// ErrInvalidSource indicates SandboxOptions did not specify exactly one source.
	ErrInvalidSource = errors.New("provide exactly one sandbox source")
	// ErrProcessClosed indicates an attached process stream is already closed.
	ErrProcessClosed = errors.New("sandbox process is closed")
	// ErrProcessExitMissing indicates the process stream ended without exit status.
	ErrProcessExitMissing = errors.New("sandbox process stream ended without exit status")
)

var (
	sandboxCapabilityUnavailableRe = regexp.MustCompile(`sandboxd ([a-z_][a-z0-9_]*) capability unavailable`)
	sandboxCapabilityOperationRe   = regexp.MustCompile(`sandboxd ([a-z_][a-z0-9_]*) [a-z-]+ failed`)
	sandboxProviderKVRe            = regexp.MustCompile(`provider=([^\s;]+)`)
	sandboxProviderStateKVRe       = regexp.MustCompile(`state=([^\s;]+)`)
	sandboxProviderReasonKVRe      = regexp.MustCompile(`reason="([^"]*)"`)
	sandboxProviderDetailRe        = regexp.MustCompile(`\b([a-z_][a-z0-9_]*) provider (available|degraded|unavailable): ([^;]+)`)
	sandboxMissingDependenciesRe   = regexp.MustCompile(`missing dependencies: ([^;]+)`)
	sandboxDependenciesRe          = regexp.MustCompile(`dependencies=([^:]+)`)
)

// ValidationError describes invalid SDK input.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("invalid %s: %s", e.Field, e.Message)
}

// RPCError wraps a gRPC status with SDK operation context.
type RPCError struct {
	Operation    string
	AllocationID string
	Code         codes.Code
	Details      string
	Retryable    bool
	Capability   *SandboxCapabilityErrorInfo
	Err          error
}

func (e *RPCError) Error() string {
	if e.AllocationID == "" {
		return fmt.Sprintf("%s failed: %s: %s", e.Operation, e.Code, e.Details)
	}
	return fmt.Sprintf("%s failed for allocation %s: %s: %s", e.Operation, e.AllocationID, e.Code, e.Details)
}

func (e *RPCError) Unwrap() error {
	return e.Err
}

func mapRPCError(err error, operation, allocationID string) error {
	if err == nil {
		return nil
	}
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) {
		return err
	}
	st, ok := status.FromError(err)
	if !ok {
		return fmt.Errorf("%s failed: %w", operation, err)
	}
	return &RPCError{
		Operation:    operation,
		AllocationID: allocationID,
		Code:         st.Code(),
		Details:      st.Message(),
		Retryable:    retryableCode(st.Code()),
		Capability:   SandboxCapabilityInfo(st.Message()),
		Err:          err,
	}
}

// ErrorRetryable reports whether an SDK error represents a transient operation
// that may be retried within the caller's original deadline.
func ErrorRetryable(err error) bool {
	var rpcErr *RPCError
	return errors.As(err, &rpcErr) && rpcErr.Retryable
}

// SandboxCapabilityErrorInfo is structured sandbox capability/provider detail parsed from an RPC error.
type SandboxCapabilityErrorInfo struct {
	Capability          string
	Provider            string
	ProviderState       string
	Reason              string
	MissingDependencies []string
}

// SandboxCapabilityInfo returns sandbox capability/provider information from an Axern RPC detail string.
func SandboxCapabilityInfo(details string) *SandboxCapabilityErrorInfo {
	if !strings.Contains(details, "sandboxd") && !strings.Contains(details, "provider") {
		return nil
	}
	capability := firstRegexMatch(sandboxCapabilityUnavailableRe, details)
	if capability == "" {
		capability = firstRegexMatch(sandboxCapabilityOperationRe, details)
	}
	provider := firstRegexMatch(sandboxProviderKVRe, details)
	providerState := firstRegexMatch(sandboxProviderStateKVRe, details)
	if !isSandboxProviderState(providerState) {
		providerState = ""
	}
	reason := firstRegexMatch(sandboxProviderReasonKVRe, details)
	if matches := sandboxProviderDetailRe.FindStringSubmatch(details); len(matches) == 4 {
		if provider == "" {
			provider = matches[1]
		}
		if providerState == "" {
			providerState = matches[2]
		}
		if reason == "" {
			reason = strings.TrimSpace(matches[3])
		}
	}
	dependencies := missingDependencyDetails(details)
	if capability == "" && provider == "" && providerState == "" && reason == "" && len(dependencies) == 0 {
		return nil
	}
	return &SandboxCapabilityErrorInfo{
		Capability:          capability,
		Provider:            provider,
		ProviderState:       providerState,
		Reason:              reason,
		MissingDependencies: dependencies,
	}
}

func isSandboxProviderState(value string) bool {
	switch value {
	case "available", "degraded", "unavailable":
		return true
	default:
		return false
	}
}

// IsValidation reports whether err was caused by invalid SDK input.
func IsValidation(err error) bool {
	if errors.Is(err, ErrInvalidSource) {
		return true
	}
	var validationErr *ValidationError
	if errors.As(err, &validationErr) {
		return true
	}
	var pathErr *PathError
	return errors.As(err, &pathErr)
}

// IsNotFound reports whether err maps to gRPC NotFound.
func IsNotFound(err error) bool {
	return errorCode(err) == codes.NotFound
}

// IsAlreadyExists reports whether err maps to gRPC AlreadyExists.
func IsAlreadyExists(err error) bool {
	return errorCode(err) == codes.AlreadyExists
}

// IsPermissionDenied reports whether err maps to permission or auth failure.
func IsPermissionDenied(err error) bool {
	code := errorCode(err)
	return code == codes.PermissionDenied || code == codes.Unauthenticated
}

// IsTimeout reports whether err is a local or remote deadline failure.
func IsTimeout(err error) bool {
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	code := errorCode(err)
	return code == codes.DeadlineExceeded
}

// IsCancelled reports whether err was cancelled by the caller or transport.
func IsCancelled(err error) bool {
	if errors.Is(err, context.Canceled) {
		return true
	}
	return errorCode(err) == codes.Canceled
}

// IsUnavailable reports whether err maps to gRPC Unavailable.
func IsUnavailable(err error) bool {
	return errorCode(err) == codes.Unavailable
}

func errorCode(err error) codes.Code {
	if err == nil {
		return codes.OK
	}
	var rpcErr *RPCError
	if errors.As(err, &rpcErr) {
		return rpcErr.Code
	}
	return status.Code(err)
}

func retryableCode(code codes.Code) bool {
	return code == codes.Unavailable || code == codes.DeadlineExceeded
}

func firstRegexMatch(expr *regexp.Regexp, value string) string {
	matches := expr.FindStringSubmatch(value)
	if len(matches) < 2 {
		return ""
	}
	return strings.TrimSpace(matches[1])
}

func missingDependencyDetails(details string) []string {
	if matches := sandboxMissingDependenciesRe.FindStringSubmatch(details); len(matches) == 2 {
		return splitNonEmpty(matches[1], ",")
	}
	matches := sandboxDependenciesRe.FindStringSubmatch(details)
	if len(matches) != 2 {
		return nil
	}
	items := splitNonEmpty(matches[1], ",")
	out := make([]string, 0, len(items))
	for _, item := range items {
		if strings.Contains(item, "=unavailable") {
			out = append(out, item)
		}
	}
	return out
}

func splitNonEmpty(value string, sep string) []string {
	parts := strings.Split(value, sep)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// ExecError is returned when ExecOptions.Check is true and a command exits nonzero.
type ExecError struct {
	Argv   []string
	Result ExecResult
}

func (e *ExecError) Error() string {
	return fmt.Sprintf("command %v exited with status %d", e.Argv, e.Result.ExitCode)
}

func (e *ExecError) ExitCode() int32 {
	if e == nil {
		return 0
	}
	return e.Result.ExitCode
}

func (e *ExecError) StdoutString() string {
	if e == nil {
		return ""
	}
	return e.Result.StdoutString()
}

func (e *ExecError) StderrString() string {
	if e == nil {
		return ""
	}
	return e.Result.StderrString()
}

func validationError(field, message string) error {
	return &ValidationError{Field: field, Message: message}
}

func requiredError(field string) error {
	return validationError(field, "is required")
}

func positiveDurationError(field string) error {
	return validationError(field, "must be greater than or equal to zero")
}

func positiveIntError(field string) error {
	return validationError(field, "must be greater than or equal to zero")
}

func modeError(field string) error {
	return validationError(field, "must fit in Unix permission bits")
}

func isBlank(value string) bool {
	return strings.TrimSpace(value) == ""
}
