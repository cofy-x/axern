package serviceproxy

import (
	"context"
	"errors"
	"net/http"
	"strings"

	nodekernel "github.com/cofy-x/axern/gateway/gatewayd/internal/kernel/nodebridge"
)

type errRequestBodyTooLarge struct{}

func (errRequestBodyTooLarge) Error() string {
	return "request body too large"
}

func isEndpointConnectError(err error) bool {
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "connect allocation port")
}

func classifyProxyError(err error) Result {
	text := strings.ToLower(err.Error())
	switch {
	case isEndpointConnectError(err):
		return Result{Status: http.StatusGatewayTimeout, ErrorClass: "timeout", Quarantine: true, EndpointRetryable: true}
	case errors.Is(err, context.DeadlineExceeded), strings.Contains(text, "timeout"), strings.Contains(text, "deadline exceeded"):
		return Result{Status: http.StatusGatewayTimeout, ErrorClass: "timeout"}
	case strings.Contains(text, "request body too large"):
		return Result{Status: http.StatusRequestEntityTooLarge, ErrorClass: "body_too_large"}
	case strings.Contains(text, "not found"), strings.Contains(text, "gone"):
		return Result{Status: http.StatusBadGateway, ErrorClass: "upstream_gone", Invalidate: true, EndpointRetryable: true}
	case nodekernel.IsExecutionLeaseRejected(err):
		return Result{Status: http.StatusBadGateway, ErrorClass: "lease", Invalidate: true, LeaseRejected: true, EndpointRetryable: true}
	default:
		return Result{Status: http.StatusBadGateway, ErrorClass: "upstream"}
	}
}
