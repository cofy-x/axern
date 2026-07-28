package handlerregistry

import (
	"context"
	"sort"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
)

func TestRegistryStatuses(t *testing.T) {
	registry := New(config.Config{
		PluginConfig: config.PluginConfig{
			RuntimeConfig: config.RuntimeConfig{
				Runtimes: map[string]config.RuntimeInstanceConfig{
					"runsc": {Binary: "/fake/runsc"},
					"runc":  {Binary: "/fake/runc"},
				},
			},
		},
	})
	registry.Set("runsc", &runtimetest.FakeRuntimeHandler{RuntimeName: "runsc"})

	statuses := registry.Statuses()
	if len(statuses) != 2 {
		t.Fatalf("Statuses() len = %d, want 2", len(statuses))
	}
	if statuses[0].Name != "runc" || statuses[1].Name != "runsc" {
		t.Fatalf("Statuses() names = %+v", statuses)
	}
	if statuses[0].Loaded {
		t.Fatalf("Statuses()[0].Loaded = true, want false")
	}
	if !statuses[1].Loaded {
		t.Fatalf("Statuses()[1].Loaded = false, want true")
	}
}

func TestRegistryNamesStableSorted(t *testing.T) {
	registry := New(config.Config{})
	registry.Set("runc", &runtimetest.FakeRuntimeHandler{RuntimeName: "runc"})
	registry.Set("runsc", &runtimetest.FakeRuntimeHandler{RuntimeName: "runsc"})
	registry.Set("crun", &runtimetest.FakeRuntimeHandler{RuntimeName: "crun"})

	names := registry.Names()
	if !sort.StringsAreSorted(names) {
		t.Fatalf("Names() returned unsorted names: %v", names)
	}
	want := []string{"crun", "runc", "runsc"}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("Names()[%d] = %q, want %q (all=%v)", i, names[i], want[i], names)
		}
	}
}

func TestRegistryVersionFallsBackToUnknown(t *testing.T) {
	registry := New(config.Config{})
	registry.Set("runsc", &runtimetest.FakeRuntimeHandler{RuntimeName: "runsc"})

	versions, err := registry.Version(context.WithValue(context.Background(), "ERROR", "boom"))
	if err != nil {
		t.Fatalf("Version() error = %v", err)
	}
	if len(versions) != 1 {
		t.Fatalf("Version() len = %d, want 1", len(versions))
	}
	if versions[0].RuntimeVersion != config.UnknownVersion {
		t.Fatalf("Version() runtime version = %q, want %q", versions[0].RuntimeVersion, config.UnknownVersion)
	}
}
