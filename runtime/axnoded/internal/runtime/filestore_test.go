package runtime

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

func TestRuntimeFilestoreHasOneLifecycleOwner(t *testing.T) {
	runtimeFilestores.Lock()
	previousStates := runtimeFilestores.byDir
	runtimeFilestores.byDir = make(map[string]*runtimeFilestoreState)
	runtimeFilestores.Unlock()
	previousPrepare, previousCleanup := prepareRuntimeFilestore, cleanupRuntimeFilestore
	t.Cleanup(func() {
		prepareRuntimeFilestore, cleanupRuntimeFilestore = previousPrepare, previousCleanup
		runtimeFilestores.Lock()
		runtimeFilestores.byDir = previousStates
		runtimeFilestores.Unlock()
	})

	prepareCalls, cleanupCalls := 0, 0
	prepareRuntimeFilestore = func(string, string, string, int64, int64) error {
		prepareCalls++
		return nil
	}
	cleanupRuntimeFilestore = func(string, string, string) error {
		cleanupCalls++
		return nil
	}
	cfg := config.Config{PluginConfig: config.PluginConfig{RuntimeConfig: config.RuntimeConfig{
		FilestoreDir:               "/var/lib/axnoded/filestore",
		FilestoreMode:              config.FilestoreModeLoopbackDev,
		FilestoreLoopbackImage:     "/var/lib/axnoded/filestore.img",
		FilestoreLoopbackSizeBytes: 1,
	}}}

	_, releaseRunc, err := acquireRuntimeFilestore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, releaseRunsc, err := acquireRuntimeFilestore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if prepareCalls != 1 {
		t.Fatalf("prepare calls = %d, want 1", prepareCalls)
	}
	releaseRunc(false)
	if cleanupCalls != 0 {
		t.Fatalf("cleanup ran while another runtime retained the filestore")
	}
	releaseRunsc(false)
	releaseRunsc(false)
	if cleanupCalls != 0 {
		t.Fatalf("handler shutdown must leave cleanup to the service owner, calls = %d", cleanupCalls)
	}
	_, abandonConstruction, err := acquireRuntimeFilestore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	abandonConstruction(true)
	if prepareCalls != 2 || cleanupCalls != 1 {
		t.Fatalf("failed construction rollback calls: prepare=%d cleanup=%d", prepareCalls, cleanupCalls)
	}
}
