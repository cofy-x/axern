package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
)

func TestJSONStoreRoundTrip(t *testing.T) {
	root := t.TempDir()
	store := NewJSONStore(root)
	in := map[string][]*runtimevolumev1.PublishedVolume{
		"alloc-1": {validPublishedVolume()},
	}
	if err := store.Save(context.Background(), in); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	out, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got := out["alloc-1"]; len(got) != 1 || got[0].GetBindingID() != "binding-1" {
		t.Fatalf("Load() = %#v, want saved volume", out)
	}
}

func TestJSONStoreRejectsCorruptState(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "published_volumes.json"), []byte("{"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err := NewJSONStore(root).Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode published volume state") {
		t.Fatalf("Load() error = %v, want decode error", err)
	}
}

func TestJSONStoreRejectsInvalidLoadedRecord(t *testing.T) {
	root := t.TempDir()
	store := NewJSONStore(root)
	if err := store.Save(context.Background(), map[string][]*runtimevolumev1.PublishedVolume{
		"alloc-1": {validPublishedVolume()},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	data := []byte(`{"alloc-1":[{"claimId":"claim-1","bindingId":"binding-1","backend":"VOLUME_BACKEND_LOCAL","hostPath":"relative","target":"/data","options":["rbind","rw"]}]}`)
	if err := os.WriteFile(filepath.Join(root, "published_volumes.json"), data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err := store.Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "absolute host path") {
		t.Fatalf("Load() error = %v, want invalid host path", err)
	}
}

func TestJSONStoreRejectsReadonlyOptionDrift(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	data := []byte(`{"alloc-1":[{"claimId":"claim-1","bindingId":"binding-1","backend":"VOLUME_BACKEND_LOCAL","hostPath":"/tmp/volume","target":"/data","readonly":false,"options":["rbind","ro"]}]}`)
	if err := os.WriteFile(filepath.Join(root, "published_volumes.json"), data, 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	_, err := NewJSONStore(root).Load(context.Background())
	if err == nil || !strings.Contains(err.Error(), "writable volume must not include ro option") {
		t.Fatalf("Load() error = %v, want readonly option drift", err)
	}
}

func TestJSONStoreRejectsInvalidSavedRecord(t *testing.T) {
	root := t.TempDir()
	store := NewJSONStore(root)
	if err := store.Save(context.Background(), map[string][]*runtimevolumev1.PublishedVolume{
		"alloc-1": {validPublishedVolume()},
	}); err != nil {
		t.Fatalf("Save() initial error = %v", err)
	}
	invalid := validPublishedVolume()
	invalid.Target = "/"
	err := store.Save(context.Background(), map[string][]*runtimevolumev1.PublishedVolume{
		"alloc-1": {invalid},
	})
	if err == nil || !strings.Contains(err.Error(), "absolute container target") {
		t.Fatalf("Save() error = %v, want invalid target", err)
	}
	out, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() after failed save error = %v", err)
	}
	if got := out["alloc-1"]; len(got) != 1 || got[0].GetTarget() != "/data" {
		t.Fatalf("Load() after failed save = %#v, want previous state", out)
	}
	if matches, err := filepath.Glob(filepath.Join(root, "published_volumes.json.*.tmp")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary files = %v err = %v, want none", matches, err)
	}
}

func TestJSONStoreHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := NewJSONStore(t.TempDir())
	if _, err := store.Load(ctx); err == nil {
		t.Fatal("Load() error = nil, want canceled context")
	}
	if err := store.Save(ctx, map[string][]*runtimevolumev1.PublishedVolume{}); err == nil {
		t.Fatal("Save() error = nil, want canceled context")
	}
}

func TestJSONStoreNormalizesSavedRecords(t *testing.T) {
	store := NewJSONStore(t.TempDir())
	item := validPublishedVolume()
	item.BindingID = " binding-1 "
	item.ClaimID = " claim-1 "
	item.HostPath = " /tmp/volume "
	item.Target = "/data/."
	item.Options = []string{"rw", "nodev", "rbind", "nodev"}
	if err := store.Save(context.Background(), map[string][]*runtimevolumev1.PublishedVolume{
		" alloc-1 ": {item},
	}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	out, err := store.Load(context.Background())
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := out["alloc-1"][0]
	if got.GetBindingID() != "binding-1" || got.GetClaimID() != "claim-1" || got.GetHostPath() != "/tmp/volume" || got.GetTarget() != "/data" {
		t.Fatalf("normalized volume = %#v", got)
	}
	if len(got.GetOptions()) != 3 || got.GetOptions()[0] != "rbind" || got.GetOptions()[1] != "nodev" || got.GetOptions()[2] != "rw" {
		t.Fatalf("normalized options = %#v", got.GetOptions())
	}
}

func validPublishedVolume() *runtimevolumev1.PublishedVolume {
	return &runtimevolumev1.PublishedVolume{
		ClaimID:   "claim-1",
		BindingID: "binding-1",
		Backend:   storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
		HostPath:  "/tmp/volume",
		Target:    "/data",
		Options:   []string{"rbind", "rw"},
	}
}
