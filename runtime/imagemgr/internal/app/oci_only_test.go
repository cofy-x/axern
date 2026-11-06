package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
)

func TestRunWithoutNydusDependencies(t *testing.T) {
	if mode := os.Getenv("IMAGEMGR_STARTUP_TEST"); mode != "" {
		root := os.Args[len(os.Args)-1]
		args := []string{"-node_id=test", "-root=" + root, "-http_sock=", "-imagefsd_bin=" + filepath.Join(root, "missing-imagefsd")}
		want := "nydus config template path is required"
		if mode == "oci-only" {
			args = append(args, "-enable_nydus=false", "-nydus_template="+filepath.Join(root, "missing-template"))
			want = "serve http: empty socket path"
		}
		// An empty socket ends Run immediately after complete initialization,
		// without needing to signal the test process or acquire mount privilege.
		if err := Run(args); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("Run() = %v, want %q", err, want)
		}
		return
	}
	for _, mode := range []string{"default", "oci-only"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			cmd := exec.Command(os.Args[0], "-test.run=^TestRunWithoutNydusDependencies$", "--", root)
			cmd.Env = append(os.Environ(), "IMAGEMGR_STARTUP_TEST="+mode)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("startup subprocess: %v\n%s", err, output)
			}
			if mode == "oci-only" {
				if _, err := os.Stat(filepath.Join(root, "oci", "metadata.db")); err != nil {
					t.Fatalf("OCI manager was not initialized: %v", err)
				}
				for _, dir := range []string{"daemon_configs", "daemons", "chunk_db", "image_metas"} {
					if _, err := os.Stat(filepath.Join(root, dir)); !os.IsNotExist(err) {
						t.Fatalf("OCI-only initialized Nydus directory %s: %v", dir, err)
					}
				}
			}
		})
	}
}

func TestRunOCIOnlyRejectsAuthorityBeforeBackendRecovery(t *testing.T) {
	for _, source := range []string{"mount", "daemon"} {
		t.Run(source, func(t *testing.T) {
			root := t.TempDir()
			var path string
			if source == "mount" {
				path = filepath.Join(root, "resource_leases.db")
				store, err := mountstore.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				_, err = store.Acquire(&mountstore.Record{CacheKey: "nydus:image", MountType: "nydus", ImageURL: "image", NydusImageURL: "image", MountPoint: "/mnt/image"}, "lease", "node")
				if err != nil {
					t.Fatal(err)
				}
				if err := store.BeginRelease("lease"); err != nil {
					t.Fatal(err)
				}
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				dir := filepath.Join(root, "daemon_configs")
				if err := os.Mkdir(dir, 0755); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(dir, "damaged.json")
				if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := Run([]string{"-node_id=test", "-root=" + root, "-enable_nydus=false"}); err == nil || !strings.Contains(err.Error(), "cannot start with Nydus disabled") {
				t.Fatalf("Run() did not fail at preflight: %v", err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("failed startup modified authority: %v", err)
			}
			for _, dir := range []string{"oci", "logs", "chunk_db"} {
				if _, err := os.Stat(filepath.Join(root, dir)); !os.IsNotExist(err) {
					t.Fatalf("recovery started before rejecting authority: %s %v", dir, err)
				}
			}
		})
	}
}
