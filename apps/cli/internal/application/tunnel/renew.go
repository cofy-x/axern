package tunnel

import (
	"context"
	"fmt"
	"time"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

const (
	defaultRenewTTL     = 30 * time.Minute
	minRenewInterval    = 30 * time.Second
	renewRequestTimeout = 10 * time.Second
	maxRenewFailures    = 3
)

type renewLoopConfig struct {
	client      TunnelClient
	sessionID   string
	clientToken string
	ttl         time.Duration
}

func startRenewLoop(ctx context.Context, cfg renewLoopConfig) <-chan error {
	done := make(chan error, 1)
	go func() {
		defer close(done)
		failures := 0
		ticker := time.NewTicker(renewInterval(cfg.ttl))
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				done <- nil
				return
			case <-ticker.C:
				renewCtx, cancel := context.WithTimeout(ctx, renewRequestTimeout)
				_, err := cfg.client.RenewTunnelSession(renewCtx, &tunnelcontrolv1.RenewTunnelSessionRequest{
					SessionID:   cfg.sessionID,
					Ttl:         durationpb.New(normalizeRenewTTL(cfg.ttl)),
					ClientToken: cfg.clientToken,
				})
				cancel()
				if err == nil {
					failures = 0
					continue
				}
				if ctx.Err() != nil {
					done <- nil
					return
				}
				if terminalRenewError(err) {
					done <- fmt.Errorf("tunnel renew failed permanently: %w", err)
					return
				}
				failures++
				if failures >= maxRenewFailures {
					done <- fmt.Errorf("tunnel renew failed %d consecutive times: %w", failures, err)
					return
				}
			}
		}
	}()
	return done
}

func terminalRenewError(err error) bool {
	switch grpcstatus.Code(err) {
	case codes.PermissionDenied, codes.NotFound, codes.FailedPrecondition, codes.Unauthenticated:
		return true
	default:
		return false
	}
}

func renewInterval(ttl time.Duration) time.Duration {
	ttl = normalizeRenewTTL(ttl)
	interval := ttl / 2
	if interval < minRenewInterval {
		return minRenewInterval
	}
	return interval
}

func normalizeRenewTTL(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return defaultRenewTTL
	}
	return ttl
}

func sessionLeaseTTL(session *tunnelcontrolv1.TunnelSession, fallback time.Duration) time.Duration {
	if session == nil || session.GetCreatedAt() == nil || session.GetExpiresAt() == nil {
		return normalizeRenewTTL(fallback)
	}
	ttl := session.GetExpiresAt().AsTime().Sub(session.GetCreatedAt().AsTime())
	if ttl <= 0 {
		return normalizeRenewTTL(fallback)
	}
	return ttl
}
