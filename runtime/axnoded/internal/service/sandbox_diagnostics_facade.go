package service

import (
	"context"
	"fmt"
)

func (h *sandboxService) SandboxCapabilityStatus(ctx context.Context, containerID string) (SandboxCapabilityStatus, error) {
	if containerID == "" {
		return SandboxCapabilityStatus{}, fmt.Errorf("sandbox capability status requires container id")
	}
	status, err := h.sandboxAccessor().SandboxCapabilityStatus(ctx, containerID)
	if err != nil {
		return SandboxCapabilityStatus{}, err
	}
	return sandboxCapabilityStatusFromSandboxAccess(status), nil
}

func (h *sandboxService) SandboxdDiagnostics(ctx context.Context, id string, full bool) (SandboxdDiagnostics, error) {
	if id == "" {
		return SandboxdDiagnostics{}, fmt.Errorf("sandboxd diagnostics requires container id")
	}
	diagnostics, err := h.sandboxAccessor().SandboxdDiagnostics(ctx, id, full)
	if err != nil {
		return SandboxdDiagnostics{}, err
	}
	return sandboxdDiagnosticsFromSandboxAccess(diagnostics), nil
}
