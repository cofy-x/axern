package nodeinventory

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCollectImagemgrInventoryDisabled(t *testing.T) {
	source := NewAxnodedSource(AxnodedSourceOptions{ImageManager: NewImageManagerClient(false, "")})
	snapshot := NewSnapshot()
	source.collectImagemgrInventory(time.Now().UTC(), &snapshot)
	require.Equal(t, StatusDisabled, snapshot.Sources["imagemgr"].Status)
	require.Equal(t, StatusDisabled, snapshot.Components.Imagemgr.Status)
}

func TestCollectImagemgrInventoryOCIOnly(t *testing.T) {
	for _, test := range []struct {
		name, response, status string
	}{
		{"OCI cache", `{"mounted_images":[{"image_url":"example.local/app:dev","cache_key":"mounted-key","mount_type":"oci","mount_path":"/mount","lease_count":1}],"imported_images":[{"image_ref":"example.local/import:dev","cache_key":"import-key"}]}`, StatusReady},
		{"retired representation", `{"mounted_images":[{"image_url":"example.local/app:dev","mount_type":"nydus","mount_path":"/retired"}],"imported_images":[]}`, StatusError},
		{"incomplete mount", `{"mounted_images":[{"image_url":"example.local/app:dev","mount_type":"oci"}],"imported_images":[]}`, StatusError},
	} {
		t.Run(test.name, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "img-")
			require.NoError(t, err)
			t.Cleanup(func() { _ = os.RemoveAll(dir) })
			socket := filepath.Join(dir, "img.sock")
			listener, err := net.Listen("unix", socket)
			require.NoError(t, err)
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/inventory" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(test.response))
			})}
			t.Cleanup(func() { _ = server.Close() })
			go func() { _ = server.Serve(listener) }()
			client := NewImageManagerClient(true, socket)
			t.Cleanup(client.client.CloseIdleConnections)
			source := NewAxnodedSource(AxnodedSourceOptions{ImageManager: client})
			snapshot := NewSnapshot()
			source.collectImagemgrInventory(time.Now().UTC(), &snapshot)
			require.Equal(t, test.status, snapshot.Components.Imagemgr.Status)
			if test.status == StatusReady {
				require.True(t, snapshot.Components.Imagemgr.Reachable)
				require.Equal(t, 1, snapshot.Components.Imagemgr.MountedImageCount)
				require.Equal(t, 1, snapshot.Components.Imagemgr.ImportedImageCount)
				require.Equal(t, 1, snapshot.Heat.MountedRootfsCount)
				require.Len(t, snapshot.Heat.Locality, 2)
			} else {
				require.False(t, snapshot.Components.Imagemgr.Reachable)
				require.Empty(t, snapshot.Heat.Locality)
			}
		})
	}
}
