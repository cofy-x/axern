package sandboxaccess

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	runtimesandboxd "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/sandboxd"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

const (
	CapabilityComputerUse = wire.CapabilityComputerUse
	CapabilityBrowser     = wire.CapabilityBrowser
	CapabilityProbe       = wire.CapabilityProbe
)

type gatewayTarget struct {
	runtimesandboxd.Target
}

func (a *Accessor) ClientForCapability(ctx context.Context, id string, capability string) (*runtimesandboxd.Client, error) {
	target, err := a.gatewayTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	diagnostics, err := a.diagnosticsSummary(ctx, target.Client)
	if err != nil {
		return nil, SandboxdCapabilityError(wire.CapabilityDiagnostics, "read", err)
	}
	latest := runtimesandboxd.SnapshotFromDiagnostics(target.Snapshot.SocketPath, diagnostics)
	if err := latest.RequireCapability(capability); err != nil {
		return nil, err
	}
	return target.Client, nil
}

func (a *Accessor) DiagnosticsWire(ctx context.Context, id string, full bool) (wire.DiagnosticsResponse, error) {
	target, err := a.gatewayTarget(ctx, id)
	if err != nil {
		return wire.DiagnosticsResponse{}, err
	}
	var diagnostics wire.DiagnosticsResponse
	if full {
		diagnostics, err = target.Client.Diagnostics(ctx)
	} else {
		diagnostics, err = a.diagnosticsSummary(ctx, target.Client)
	}
	if err != nil {
		return wire.DiagnosticsResponse{}, SandboxdCapabilityError(wire.CapabilityDiagnostics, "read", err)
	}
	return diagnostics, nil
}

func (a *Accessor) gatewayTarget(ctx context.Context, id string) (gatewayTarget, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if id == "" {
		return gatewayTarget{}, errord.ErrInvalidArgument
	}
	target, err := a.runningTarget(id)
	if err != nil {
		return gatewayTarget{}, err
	}
	sandboxdTarget, err := runtimesandboxd.TargetFromLabels(target.Labels)
	if err != nil {
		return gatewayTarget{}, err
	}
	return gatewayTarget{Target: sandboxdTarget}, nil
}

func (a *Accessor) diagnosticsSummary(ctx context.Context, client *runtimesandboxd.Client) (wire.DiagnosticsResponse, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	diagnosticsCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	return client.DiagnosticsSummary(diagnosticsCtx)
}

func (a *Accessor) DiagnosticsFailureDetail(ctx context.Context, id string) string {
	diagnostics, err := a.DiagnosticsWire(ctx, id, false)
	if err != nil {
		return ""
	}
	parts := make([]string, 0, 3)
	if state := strings.TrimSpace(diagnostics.Status.UserProcess.State); state != "" {
		parts = append(parts, "user process state="+state)
	}
	if diagnostics.ProviderSummary.Total > 0 {
		parts = append(parts, fmt.Sprintf("providers %d/%d available", diagnostics.ProviderSummary.Available, diagnostics.ProviderSummary.Total))
	}
	for _, provider := range diagnostics.Providers {
		if provider.Available && provider.State == "available" {
			continue
		}
		parts = append(parts, SandboxdProviderFailureDetail(provider))
	}
	return strings.Join(parts, "; ")
}

func SandboxdProviderFailureDetail(provider wire.CapabilityProvider) string {
	name := strings.TrimSpace(provider.Name)
	if name == "" {
		name = "unknown"
	}
	state := strings.TrimSpace(provider.State)
	if state == "" {
		state = "unavailable"
	}
	reason := strings.TrimSpace(provider.Reason)
	if reason == "" {
		reason = state
	}
	detail := name + " provider " + state + ": " + reason
	if dependencies := sandboxdUnavailableDependencyDetail(provider.Dependencies); dependencies != "" {
		detail += "; " + dependencies
	}
	return detail
}

func sandboxdUnavailableDependencyDetail(dependencies []wire.ProviderDependency) string {
	parts := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		if dependency.Available {
			continue
		}
		name := strings.TrimSpace(dependency.Name)
		if name == "" {
			name = "unknown"
		}
		reason := strings.TrimSpace(dependency.Reason)
		if reason == "" {
			parts = append(parts, name)
			continue
		}
		parts = append(parts, name+" ("+reason+")")
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return "missing dependencies: " + strings.Join(parts, ", ")
}

func (a *Accessor) OperationError(ctx context.Context, id string, capability string, operation string, err error) error {
	return a.ResourceOperationError(ctx, id, capability, operation, "", err)
}

func (a *Accessor) ResourceOperationError(ctx context.Context, id string, capability string, operation string, resource string, err error) error {
	if err == nil {
		return nil
	}
	wrapped := runtimesandboxd.ResourceOperationError(capability, operation, resource, err)
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return wrapped
	}
	if detail := a.DiagnosticsFailureDetail(ctx, id); detail != "" {
		return fmt.Errorf("%w; sandboxd %s", wrapped, detail)
	}
	return wrapped
}

func SandboxdCapabilityError(capability string, operation string, err error) error {
	return runtimesandboxd.OperationError(capability, operation, err)
}
