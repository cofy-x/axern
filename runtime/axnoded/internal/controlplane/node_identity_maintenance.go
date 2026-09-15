package controlplane

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"strings"
	"time"
)

// Bootstrap reads deployment-owned input only when no identity has been
// published. It never deletes a projected Secret or re-enrolls an invalid key.
func (n *NodeEnrollment) Bootstrap(ctx context.Context, tokenFile string) error {
	if _, err := os.Stat(n.BundlePath); err == nil {
		return n.Ensure(ctx, "")
	} else if !os.IsNotExist(err) {
		return err
	}
	f, err := os.Open(tokenFile)
	if err != nil {
		return fmt.Errorf("read node bootstrap token file: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil {
		return err
	}
	token := strings.TrimSpace(string(data))
	if token == "" || len(data) > 4096 {
		return fmt.Errorf("invalid node bootstrap token file")
	}
	return n.Ensure(ctx, token)
}

// Maintain has one owner and no persisted queue. Registration and renewal
// failure never block Allocation recovery or extend an ExecutionLease.
func (n *NodeEnrollment) Maintain(ctx context.Context, tokenFile string, report func(error)) {
	failures := 0
	for {
		attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
		err := n.Bootstrap(attempt, tokenFile)
		cancel()
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			break
		}
		report(err)
		if !waitIdentityDeadline(ctx, time.Now().Add(identityRetryDelay(failures, rand.Float64()))) {
			return
		}
		failures++
	}
	// Drop bootstrap input from the runtime maintenance path. The deployment
	// owner can now remove its Secret without affecting renewal or restart.
	tokenFile = ""
	failures = 0
	var due time.Time
	for {
		pair, err := n.current()
		if err == nil {
			if due.IsZero() {
				due = identityRenewalDeadline(pair.Leaf.NotAfter, rand.Float64())
			}
			if !waitIdentityDeadline(ctx, due) {
				return
			}
			attempt, cancel := context.WithTimeout(ctx, 10*time.Second)
			err = n.RenewIfDue(attempt)
			cancel()
		}
		if ctx.Err() != nil {
			return
		}
		if err == nil {
			failures = 0
			due = time.Time{}
			continue
		}
		report(err)
		if !waitIdentityDeadline(ctx, time.Now().Add(identityRetryDelay(failures, rand.Float64()))) {
			return
		}
		failures++
	}
}

func identityRenewalDeadline(expires time.Time, jitter float64) time.Time {
	return expires.Add(-8 * time.Hour).Add(time.Duration(jitter * float64(time.Hour)))
}

func identityRetryDelay(failures int, jitter float64) time.Duration {
	cap := time.Second
	for i := 0; i < failures && cap < time.Minute; i++ {
		cap *= 2
	}
	if cap > time.Minute {
		cap = time.Minute
	}
	return cap/2 + time.Duration(jitter*float64(cap/2))
}

// Recheck wall-clock time so clock corrections do not turn a long monotonic
// sleep into renewal after expiry. No certificate facts are persisted here.
func waitIdentityDeadline(ctx context.Context, deadline time.Time) bool {
	for {
		delay := time.Until(deadline)
		if delay <= 0 {
			return ctx.Err() == nil
		}
		if delay > time.Minute {
			delay = time.Minute
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		}
	}
}
