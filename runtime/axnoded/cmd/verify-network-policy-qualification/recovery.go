package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/egress"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
)

// This is an observation cadence, not a guarantee on scheduler latency or the
// daemon's internal recovery time. Do not compare it with the old 25ms method.
const recoveryProbeInterval = time.Millisecond

func waitEgressClientContext(ctx context.Context, socket string) (*egress.Client, error) {
	var ready *egress.Client
	err := waitRecoveryHealth(ctx, func(probeCtx context.Context) (bool, error) {
		client, err := egress.Dial(probeCtx, socket)
		if err != nil {
			return false, err
		}
		health, err := client.Health(probeCtx)
		if err == nil && health.GetStatus() == runtimeegressv1.EgressManagerStatus_EGRESS_MANAGER_STATUS_OK {
			ready = client
			return true, nil
		}
		_ = client.Close()
		return false, err
	})
	if err != nil {
		if ready != nil {
			_ = ready.Close()
		}
		return nil, fmt.Errorf("egressd readiness observation failed: %w", err)
	}
	return ready, nil
}

func waitRecoveryHealth(ctx context.Context, probe func(context.Context) (bool, error)) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		probeCtx, cancel := context.WithTimeout(ctx, 250*time.Millisecond)
		ready, _ := probe(probeCtx)
		cancel()
		if err := ctx.Err(); err != nil {
			return err
		}
		if ready {
			return nil
		}
		timer := time.NewTimer(recoveryProbeInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Keep ordered raw observations outside the aggregate schema. This diagnostic
// file contains neither allocation identifiers nor destination information.
func writeRecoveryObservations(path string, values []float64, complete bool) error {
	data, err := json.MarshalIndent(struct {
		Method          string    `json:"method"`
		ProbeIntervalMS float64   `json:"probeIntervalMilliseconds"`
		Complete        bool      `json:"complete"`
		Values          []float64 `json:"observedMilliseconds"`
	}{"client-health-and-recovered-proof-v2", milliseconds(recoveryProbeInterval), complete, values}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}
