package nodeinventory

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
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

func TestCollectImagemgrInventoryImagefsdMode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		field  string
		status string
	}{
		{name: "disabled", field: `"imagefsd_enabled":false,`, status: StatusDisabled},
		{name: "enabled", field: `"imagefsd_enabled":true,`, status: StatusReady},
		{name: "legacy", status: StatusReady},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Keep the address short enough for Unix sockets on every test host.
			dir, err := os.MkdirTemp("", "img-")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			socket := filepath.Join(dir, "img.sock")
			listener, err := net.Listen("unix", socket)
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/inventory" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{%s"mounted_images":[{"image_url":"example.local/app:dev","mount_type":"oci","mount_path":"/mount"}],"imported_images":[{"image_ref":"example.local/app:dev"}],"locality":[{"key":"image:example.local/app:dev","mount_type":"oci","mounted":true}],"daemons":[]}`, tc.field)
			})}
			t.Cleanup(func() { _ = server.Close() })
			go func() { _ = server.Serve(listener) }()
			client := NewImageManagerClient(true, socket)
			t.Cleanup(client.client.CloseIdleConnections)
			source := NewAxnodedSource(AxnodedSourceOptions{ImageManager: client})
			snapshot := NewSnapshot()
			source.collectImagemgrInventory(time.Now().UTC(), &snapshot)

			if snapshot.Components.Imagemgr.Status != StatusReady || !snapshot.Components.Imagemgr.Reachable {
				t.Fatalf("imagemgr must remain ready: %+v", snapshot.Components.Imagemgr)
			}
			if snapshot.Components.Imagemgr.MountedImageCount != 1 || snapshot.Components.Imagemgr.ImportedImageCount != 1 || len(snapshot.Heat.Locality) != 1 || snapshot.Heat.MountedRootfsCount != 1 {
				t.Fatalf("OCI inventory lost: %+v", snapshot)
			}
			if snapshot.Sources["imagefsd"].Status != tc.status || snapshot.Components.Imagefsd.Status != tc.status {
				t.Fatalf("imagefsd source/component = %+v / %+v, want %s", snapshot.Sources["imagefsd"], snapshot.Components.Imagefsd, tc.status)
			}
			if snapshot.Components.Imagefsd.Reachable != (tc.status == StatusReady) {
				t.Fatalf("imagefsd reachable = %v for %s", snapshot.Components.Imagefsd.Reachable, tc.name)
			}
		})
	}
}
