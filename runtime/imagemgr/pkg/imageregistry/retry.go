package imageregistry

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

const registryFetchRetryAttempts = 3

var (
	registryRetryDelay    = time.Second
	registryRetryMaxDelay = 5 * time.Second
)

func shouldRetryRegistryRequest(req *http.Request, resp *http.Response, err error, attempt int) bool {
	if attempt >= registryFetchRetryAttempts-1 {
		return false
	}
	if req == nil || !isRetryableRegistryMethod(req.Method) {
		return false
	}
	if err != nil {
		return shouldRetryRegistryError(err)
	}
	return shouldRetryRegistryResponse(resp)
}

func isRetryableRegistryMethod(method string) bool {
	return method == http.MethodGet || method == http.MethodHead
}

func shouldRetryRegistryResponse(resp *http.Response) bool {
	return resp != nil && resp.StatusCode >= http.StatusInternalServerError
}

func shouldRetryRegistryError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "tls handshake timeout") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "server sent goaway") ||
		strings.Contains(msg, "unexpected eof")
}

func closeRegistryRetryState(resp *http.Response, conn net.Conn) {
	if resp != nil && resp.Body != nil {
		_ = resp.Body.Close()
	}
	if conn != nil {
		_ = conn.Close()
	}
}

func nextRegistryRetryDelay(attempt int) time.Duration {
	delay := registryRetryDelay << attempt
	if delay > registryRetryMaxDelay {
		return registryRetryMaxDelay
	}
	return delay
}

func waitForRegistryRetry(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func describeRegistryRetry(resp *http.Response, err error) string {
	if err != nil {
		return err.Error()
	}
	if resp == nil {
		return "unknown registry error"
	}
	return fmt.Sprintf("unexpected status %d", resp.StatusCode)
}

func shouldCloseConnOnServerError(resp *http.Response) bool {
	return resp != nil &&
		resp.StatusCode >= http.StatusInternalServerError &&
		resp.ProtoMajor == 1
}
