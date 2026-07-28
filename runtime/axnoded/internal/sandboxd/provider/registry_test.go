package provider

import (
	"reflect"
	"testing"
)

func TestRegistryCapabilitiesIncludeDispatchAvailableProviders(t *testing.T) {
	registry := New(
		Static("core", CapabilityStatus, CapabilityHealth, CapabilityStatus),
		Unavailable(CapabilityComputerUse, "not configured", CapabilityComputerUse),
		Degraded(CapabilityBrowser, "window manager unavailable", CapabilityBrowser),
		Static("process", CapabilityProcess, CapabilityPTY),
	)

	if got, want := registry.Capabilities(), []string{CapabilityBrowser, CapabilityHealth, CapabilityProcess, CapabilityPTY, CapabilityStatus}; !reflect.DeepEqual(got, want) {
		t.Fatalf("capabilities = %#v, want %#v", got, want)
	}

	providers := registry.Providers()
	if got, want := len(providers), 4; got != want {
		t.Fatalf("provider count = %d, want %d", got, want)
	}
	if providers[0].Name != CapabilityBrowser || providers[0].State != ProviderStateDegraded || !providers[0].Available {
		t.Fatalf("providers = %#v", providers)
	}
	if providers[1].Name != CapabilityComputerUse || providers[1].Available {
		t.Fatalf("providers = %#v", providers)
	}
	if providers[1].State != ProviderStateUnavailable || providers[2].State != ProviderStateAvailable {
		t.Fatalf("provider states = %#v", providers)
	}
	snapshot := registry.Snapshot()
	if !reflect.DeepEqual(snapshot.Capabilities, []string{CapabilityBrowser, CapabilityHealth, CapabilityProcess, CapabilityPTY, CapabilityStatus}) {
		t.Fatalf("snapshot capabilities = %#v", snapshot.Capabilities)
	}
	if snapshot.Summary.Total != 4 || snapshot.Summary.Available != 2 || snapshot.Summary.Degraded != 1 || snapshot.Summary.Unavailable != 1 {
		t.Fatalf("snapshot summary = %#v", snapshot.Summary)
	}
}

func TestAvailableProviderWithUnavailableDependencyIsDegraded(t *testing.T) {
	registry := New(Static(CapabilityBrowser, CapabilityBrowser).WithDependencies(
		Dependency{Name: "browser_command", Available: true},
		Dependency{Name: "window_manager", Available: false, Reason: "not ready"},
	))

	providers := registry.Providers()
	if len(providers) != 1 {
		t.Fatalf("providers = %#v", providers)
	}
	if providers[0].State != ProviderStateDegraded || !providers[0].Available {
		t.Fatalf("provider = %#v, want degraded but dispatch-available", providers[0])
	}
	if providers[0].Reason != "window_manager unavailable: not ready" {
		t.Fatalf("reason = %q", providers[0].Reason)
	}
	if got, want := registry.Capabilities(), []string{CapabilityBrowser}; !reflect.DeepEqual(got, want) {
		t.Fatalf("capabilities = %#v, want %#v", got, want)
	}
}

func TestRegistryAddReplacesProviderByName(t *testing.T) {
	registry := New(Static("process", CapabilityProcess))
	registry.Add(Static("process", CapabilityProcess, CapabilityPTY))

	providers := registry.Providers()
	if got, want := len(providers), 1; got != want {
		t.Fatalf("provider count = %d, want %d", got, want)
	}
	if got, want := providers[0].Capabilities, []string{CapabilityProcess, CapabilityPTY}; !reflect.DeepEqual(got, want) {
		t.Fatalf("capabilities = %#v, want %#v", got, want)
	}
}

func TestProviderOptionalContractFields(t *testing.T) {
	item := Static(CapabilityBrowser, CapabilityBrowser).
		WithBackend("desktop").
		WithCommand("chromium").
		WithDependencies(Dependency{Name: "browser_command", Available: true})
	item.LastError = "window closed"

	if item.Backend != "desktop" || item.Command != "chromium" || item.LastError != "window closed" {
		t.Fatalf("provider metadata = %#v", item)
	}
	if got, want := item.Dependencies, []Dependency{{Name: "browser_command", Available: true}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("dependencies = %#v, want %#v", got, want)
	}
}
