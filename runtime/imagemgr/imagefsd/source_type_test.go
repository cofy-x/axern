package imagefsd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRemovedSourcesRejectedBeforeCacheReuse(t *testing.T) {
	d := newTestDaemon("existing")
	d.mountFailed.Store(true)
	mgr := newTestManager(map[string]*Daemon{"existing": d})
	for _, source := range []string{"", "invalid"} {
		if err := mgr.CreateDaemon(&DaemonCreateOpt{ID: "existing", SourceType: source}); err == nil {
			t.Fatalf("accepted unsupported source %q", source)
		}
		if !d.mountFailed.Load() {
			t.Fatal("invalid request mutated cached daemon")
		}
		invalid := &Daemon{meta: DaemonMeta{SourceType: source}}
		if args := invalid.buildMountArgs(); args != nil {
			t.Fatalf("built mount command for unsupported source %q: %v", source, args)
		}
	}
}

func TestLoadRejectsInvalidDaemonWithoutModifyingRecord(t *testing.T) {
	for _, source := range []string{"", "invalid"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			configDir := filepath.Join(root, "daemon_configs")
			if err := os.MkdirAll(configDir, 0755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(configDir, "invalid.json")
			data := []byte(`{"id":"invalid","source_type":"` + source + `"}`)
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			mgr := &manager{ctx: context.Background(), root: root, daemons: map[string]*Daemon{}}
			err := mgr.loadExistedDaemons()
			if err == nil || !strings.Contains(err.Error(), "unsupported persisted daemon source") {
				t.Fatalf("invalid source error = %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(data) {
				t.Fatalf("invalid metadata changed: %v", err)
			}
			if len(mgr.daemons) != 0 {
				t.Fatal("invalid daemon was registered")
			}
		})
	}
}
