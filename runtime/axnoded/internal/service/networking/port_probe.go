package networking

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

// ProbePort verifies that a TCP endpoint is reachable inside an Allocation.
// It is a readiness primitive; inbound user access is owned by TunnelSession.
func (c *Coordinator) ProbePort(ctx context.Context, allocationID string, port int32) error {
	if c == nil || strings.TrimSpace(allocationID) == "" || port <= 0 || port > 65535 {
		return errord.ErrInvalidArgument
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ip, err := c.ContainerIP(allocationID)
	if err != nil {
		return err
	}
	conn, err := c.connectPort(ctx, net.JoinHostPort(ip, strconv.Itoa(int(port))))
	if err != nil {
		return fmt.Errorf("connect allocation port: %w", err)
	}
	return conn.Close()
}

func (c *Coordinator) connectPort(ctx context.Context, address string) (net.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	connectCtx := ctx
	cancel := func() {}
	if _, ok := ctx.Deadline(); !ok && c.connectTimeout > 0 {
		connectCtx, cancel = context.WithTimeout(ctx, c.connectTimeout)
	}
	defer cancel()

	for {
		conn, dialErr := c.dialContext(connectCtx, "tcp", address)
		if dialErr == nil {
			return conn, nil
		}
		if connectCtx.Err() != nil || !retryablePortConnectError(dialErr) {
			return nil, dialErr
		}
		if err := sleepContext(connectCtx, c.connectRetryDelay); err != nil {
			return nil, dialErr
		}
	}
}

func retryablePortConnectError(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "connection refused") || strings.Contains(text, "i/o timeout") ||
		strings.Contains(text, "no route to host") || strings.Contains(text, "network is unreachable")
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
