package provider

import (
	"sort"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

const (
	ProviderStateAvailable   = wire.ProviderStateAvailable
	ProviderStateDegraded    = wire.ProviderStateDegraded
	ProviderStateUnavailable = wire.ProviderStateUnavailable

	ProviderNameCore    = "core"
	ProviderNameFile    = "file"
	ProviderNameProcess = "process"

	CapabilityHealth       = wire.CapabilityHealth
	CapabilityStatus       = wire.CapabilityStatus
	CapabilitySupervisor   = wire.CapabilitySupervisor
	CapabilityDiagnostics  = wire.CapabilityDiagnostics
	CapabilityFile         = wire.CapabilityFile
	CapabilityArchive      = wire.CapabilityArchive
	CapabilityProcess      = wire.CapabilityProcess
	CapabilityManagedProxy = wire.CapabilityManagedProxy
	CapabilityPTY          = wire.CapabilityPTY
	CapabilityProbe        = wire.CapabilityProbe
	CapabilityPorts        = wire.CapabilityPorts
	CapabilityMounts       = wire.CapabilityMounts
	CapabilityComputerUse  = wire.CapabilityComputerUse
	CapabilityBrowser      = wire.CapabilityBrowser
)

type Provider struct {
	Name         string       `json:"name"`
	State        string       `json:"state,omitempty"`
	Available    bool         `json:"available"`
	Capabilities []string     `json:"capabilities,omitempty"`
	Backend      string       `json:"backend,omitempty"`
	Command      string       `json:"command,omitempty"`
	Reason       string       `json:"reason,omitempty"`
	LastError    string       `json:"lastError,omitempty"`
	Dependencies []Dependency `json:"dependencies,omitempty"`
}

type Dependency struct {
	Name      string `json:"name"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type Registry struct {
	providers []Provider
}

type Snapshot struct {
	Capabilities []string
	Providers    []Provider
	Summary      ProviderSummary
}

type ProviderSummary struct {
	Total       int
	Available   int
	Degraded    int
	Unavailable int
}

func New(providers ...Provider) Registry {
	registry := Registry{}
	for _, item := range providers {
		registry.Add(item)
	}
	return registry
}

func Static(name string, capabilities ...string) Provider {
	return Provider{Name: name, State: ProviderStateAvailable, Available: true, Capabilities: capabilities}
}

func Unavailable(name string, reason string, capabilities ...string) Provider {
	return Provider{Name: name, State: ProviderStateUnavailable, Available: false, Reason: reason, Capabilities: capabilities}
}

func Degraded(name string, reason string, capabilities ...string) Provider {
	return Provider{Name: name, State: ProviderStateDegraded, Available: true, Reason: reason, Capabilities: capabilities}
}

func (p Provider) WithBackend(backend string) Provider {
	p.Backend = backend
	return p
}

func (p Provider) WithCommand(command string) Provider {
	p.Command = command
	return p
}

func (p Provider) WithDependencies(dependencies ...Dependency) Provider {
	p.Dependencies = dependencies
	return p
}

func (r *Registry) Add(item Provider) {
	if item.Name == "" {
		return
	}
	item = normalizeProvider(item)
	for i, existing := range r.providers {
		if existing.Name == item.Name {
			r.providers[i] = item
			r.sort()
			return
		}
	}
	r.providers = append(r.providers, item)
	r.sort()
}

func (r *Registry) sort() {
	sort.SliceStable(r.providers, func(i, j int) bool {
		return r.providers[i].Name < r.providers[j].Name
	})
}

func (r Registry) Providers() []Provider {
	out := make([]Provider, len(r.providers))
	copy(out, r.providers)
	return out
}

func (r Registry) Snapshot() Snapshot {
	providers := r.Providers()
	summary := ProviderSummary{Total: len(providers)}
	for _, item := range providers {
		switch item.State {
		case ProviderStateAvailable:
			summary.Available++
		case ProviderStateDegraded:
			summary.Degraded++
		default:
			summary.Unavailable++
		}
	}
	return Snapshot{
		Capabilities: r.Capabilities(),
		Providers:    providers,
		Summary:      summary,
	}
}

func (r Registry) Capabilities() []string {
	set := map[string]struct{}{}
	for _, item := range r.providers {
		if !item.Available {
			continue
		}
		for _, capability := range item.Capabilities {
			if capability != "" {
				set[capability] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for capability := range set {
		out = append(out, capability)
	}
	sort.Strings(out)
	return out
}

func normalizeProvider(item Provider) Provider {
	unavailableDependency := firstUnavailableDependency(item.Dependencies)
	switch {
	case !item.Available:
		item.State = ProviderStateUnavailable
	case item.State == ProviderStateDegraded || unavailableDependency != nil:
		item.State = ProviderStateDegraded
		if item.Reason == "" && unavailableDependency != nil {
			item.Reason = dependencyUnavailableReason(*unavailableDependency)
		}
	default:
		item.State = ProviderStateAvailable
	}
	capabilitySet := map[string]struct{}{}
	for _, capability := range item.Capabilities {
		if capability != "" {
			capabilitySet[capability] = struct{}{}
		}
	}
	item.Capabilities = item.Capabilities[:0]
	for capability := range capabilitySet {
		item.Capabilities = append(item.Capabilities, capability)
	}
	sort.Strings(item.Capabilities)
	return item
}

func firstUnavailableDependency(dependencies []Dependency) *Dependency {
	for i := range dependencies {
		if !dependencies[i].Available {
			return &dependencies[i]
		}
	}
	return nil
}

func dependencyUnavailableReason(dependency Dependency) string {
	if dependency.Reason == "" {
		return dependency.Name + " unavailable"
	}
	return dependency.Name + " unavailable: " + dependency.Reason
}
