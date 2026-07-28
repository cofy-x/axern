package sandboxd

import (
	"context"
	"fmt"
	"strings"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

type ReadyWaiter func(context.Context, string, *apipb.ContainerMetadata) error

const (
	// Runtime startup may include a cold lazy-image mount and runsc initialization.
	// Keep a bounded readiness window while allowing the caller context to cancel
	// earlier when the allocation itself has a tighter deadline.
	DefaultReadyTimeout = 30 * time.Second
	DefaultPollInterval = 50 * time.Millisecond
)

func WaitReadyForContainer(ctx context.Context, bundlePath string, meta *apipb.ContainerMetadata) error {
	if meta == nil {
		return fmt.Errorf("sandboxd ready check requires container metadata")
	}
	socketPath := runtimeoci.SandboxdBundleSocketPath(bundlePath)
	client := NewClient(socketPath)
	snapshot, err := client.WaitReady(ctx, DefaultReadyTimeout, DefaultPollInterval)
	if err != nil {
		return fmt.Errorf("sandboxd ready check failed for %s: %w", socketPath, err)
	}
	meta.Labels = EnrichLabels(meta.Labels, socketPath, snapshot)
	return nil
}

func (c *Client) WaitReady(ctx context.Context, timeout, pollInterval time.Duration) (wire.ReadySnapshot, error) {
	if timeout <= 0 {
		timeout = DefaultReadyTimeout
	}
	if pollInterval <= 0 {
		pollInterval = DefaultPollInterval
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	var lastErr error
	for {
		diagnostics, err := c.DiagnosticsSummary(ctx)
		switch {
		case err != nil:
			if ctx.Err() == nil || lastErr == nil {
				lastErr = err
			}
		case !diagnostics.Ready:
			lastErr = fmt.Errorf("sandboxd diagnostics ready response was false")
		case diagnostics.ProtocolVersion != wire.ProtocolVersion:
			lastErr = fmt.Errorf("sandboxd diagnostics protocol version = %d, want %d", diagnostics.ProtocolVersion, wire.ProtocolVersion)
		default:
			return readySnapshotFromDiagnostics(diagnostics), nil
		}

		select {
		case <-ctx.Done():
			if lastErr != nil {
				return wire.ReadySnapshot{}, fmt.Errorf("sandboxd ready timed out at %s: %w", c.socketPath, lastErr)
			}
			return wire.ReadySnapshot{}, fmt.Errorf("sandboxd ready timed out at %s: %w", c.socketPath, ctx.Err())
		case <-ticker.C:
		}
	}
}

func readySnapshotFromDiagnostics(diagnostics wire.DiagnosticsResponse) wire.ReadySnapshot {
	return wire.ReadySnapshot{
		Ready:  wire.ReadyResponse{ProtocolVersion: diagnostics.ProtocolVersion, Ready: diagnostics.Ready},
		Status: diagnostics.Status,
		Capabilities: wire.CapabilitiesResponse{
			ProtocolVersion: diagnostics.ProtocolVersion,
			Capabilities:    diagnostics.Capabilities,
			Providers:       diagnostics.Providers,
			Summary:         diagnostics.ProviderSummary,
		},
	}
}

func EnrichLabels(labels map[string]string, socketPath string, snapshot wire.ReadySnapshot) map[string]string {
	if labels == nil {
		labels = map[string]string{}
	}
	capabilities := SnapshotFromReady(socketPath, snapshot).CapabilityList()
	labels[LabelReady] = "true"
	labels[LabelSocket] = socketPath
	labels[LabelCapabilities] = strings.Join(capabilities, ",")
	labels[LabelUserState] = snapshot.Status.UserProcess.State
	return labels
}
