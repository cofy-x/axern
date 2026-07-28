package nodeinventory

import (
	"testing"
	"time"
)

func TestCollectImagemgrInventoryDisabled(t *testing.T) {
	source := NewAxnodedSource(AxnodedSourceOptions{
		ImageManager: NewImageManagerClient(false, ""),
	})
	snapshot := NewSnapshot()

	source.collectImagemgrInventory(time.Now().UTC(), &snapshot)

	if snapshot.Sources["imagemgr"].Status != StatusDisabled {
		t.Fatalf("imagemgr source status = %q, want %q", snapshot.Sources["imagemgr"].Status, StatusDisabled)
	}
	if snapshot.Sources["imagefsd"].Status != StatusDisabled {
		t.Fatalf("imagefsd source status = %q, want %q", snapshot.Sources["imagefsd"].Status, StatusDisabled)
	}
	if snapshot.Components.Imagemgr.Status != StatusDisabled {
		t.Fatalf("imagemgr component status = %q, want %q", snapshot.Components.Imagemgr.Status, StatusDisabled)
	}
	if snapshot.Components.Imagefsd.Status != StatusDisabled {
		t.Fatalf("imagefsd component status = %q, want %q", snapshot.Components.Imagefsd.Status, StatusDisabled)
	}
}
