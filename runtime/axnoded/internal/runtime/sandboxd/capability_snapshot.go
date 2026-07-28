package sandboxd

import (
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type CapabilitySnapshot struct {
	Ready        bool
	SocketPath   string
	UserState    string
	Capabilities map[string]struct{}
	Providers    map[string]wire.CapabilityProvider
}

func SnapshotFromReady(socketPath string, snapshot wire.ReadySnapshot) CapabilitySnapshot {
	return newCapabilitySnapshot(true, socketPath, snapshot.Status.UserProcess.State, snapshot.Capabilities.Capabilities, snapshot.Capabilities.Providers)
}

func SnapshotFromDiagnostics(socketPath string, diagnostics wire.DiagnosticsResponse) CapabilitySnapshot {
	return newCapabilitySnapshot(diagnostics.Ready, socketPath, diagnostics.Status.UserProcess.State, diagnostics.Capabilities, diagnostics.Providers)
}

func SnapshotFromLabels(labels map[string]string) (CapabilitySnapshot, error) {
	if labels == nil {
		return CapabilitySnapshot{}, fmt.Errorf("sandboxd metadata labels are missing: %w", errord.ErrFailedPrecondition)
	}
	socketPath := strings.TrimSpace(labels[LabelSocket])
	if socketPath == "" {
		return CapabilitySnapshot{}, fmt.Errorf("sandboxd socket is empty: %w", errord.ErrFailedPrecondition)
	}
	return newCapabilitySnapshot(labels[LabelReady] == "true", socketPath, labels[LabelUserState], splitCapabilityLabel(labels[LabelCapabilities]), nil), nil
}

func requireCapabilityFromLabels(labels map[string]string, capability string) error {
	snapshot, err := SnapshotFromLabels(labels)
	if err != nil {
		return err
	}
	return snapshot.RequireCapability(capability)
}

func newCapabilitySnapshot(ready bool, socketPath string, userState string, capabilities []string, providers []wire.CapabilityProvider) CapabilitySnapshot {
	snapshot := CapabilitySnapshot{
		Ready:        ready,
		SocketPath:   strings.TrimSpace(socketPath),
		UserState:    userState,
		Capabilities: make(map[string]struct{}, len(capabilities)),
		Providers:    make(map[string]wire.CapabilityProvider, len(providers)),
	}
	for _, capability := range capabilities {
		capability = strings.TrimSpace(capability)
		if capability != "" {
			snapshot.Capabilities[capability] = struct{}{}
		}
	}
	for _, item := range providers {
		if strings.TrimSpace(item.Name) != "" {
			snapshot.Providers[item.Name] = item
		}
	}
	return snapshot
}

func (s CapabilitySnapshot) HasCapability(capability string) bool {
	_, ok := s.Capabilities[capability]
	return ok
}

func (s CapabilitySnapshot) ProviderForCapability(capability string) (wire.CapabilityProvider, bool) {
	capability = strings.TrimSpace(capability)
	if capability == "" {
		return wire.CapabilityProvider{}, false
	}
	for _, item := range s.Providers {
		if slices.Contains(item.Capabilities, capability) {
			return item, true
		}
	}
	return wire.CapabilityProvider{}, false
}

func (s CapabilitySnapshot) RequireReady() error {
	if !s.Ready {
		return fmt.Errorf("sandboxd unavailable: ready=false: %w", errord.ErrFailedPrecondition)
	}
	if s.SocketPath == "" {
		return fmt.Errorf("sandboxd unavailable: socket is empty: %w", errord.ErrFailedPrecondition)
	}
	return nil
}

func (s CapabilitySnapshot) RequireCapability(capability string) error {
	if err := s.RequireReady(); err != nil {
		return err
	}
	if provider, ok := s.ProviderForCapability(capability); ok && !provider.Available {
		return fmt.Errorf("sandboxd %s capability unavailable: provider=%s state=%s reason=%q dependencies=%s: %w", capability, provider.Name, provider.State, provider.Reason, providerDependencySummary(provider.Dependencies), errord.ErrFailedPrecondition)
	}
	if !s.HasCapability(capability) {
		if provider, ok := s.ProviderForCapability(capability); ok {
			return fmt.Errorf("sandboxd %s capability unavailable: provider=%s state=%s available=%t reason=%q capabilities=%q: %w", capability, provider.Name, provider.State, provider.Available, provider.Reason, strings.Join(s.CapabilityList(), ","), errord.ErrFailedPrecondition)
		}
		return fmt.Errorf("sandboxd %s capability unavailable: provider not reported, capabilities=%q: %w", capability, strings.Join(s.CapabilityList(), ","), errord.ErrFailedPrecondition)
	}
	return nil
}

func (s CapabilitySnapshot) CapabilityList() []string {
	out := make([]string, 0, len(s.Capabilities))
	for capability := range s.Capabilities {
		out = append(out, capability)
	}
	sort.Strings(out)
	return out
}

func splitCapabilityLabel(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func providerDependencySummary(dependencies []wire.ProviderDependency) string {
	if len(dependencies) == 0 {
		return ""
	}
	parts := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		state := "available"
		if !dependency.Available {
			state = "unavailable"
		}
		if dependency.Reason != "" {
			parts = append(parts, dependency.Name+"="+state+"("+dependency.Reason+")")
			continue
		}
		parts = append(parts, dependency.Name+"="+state)
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}
