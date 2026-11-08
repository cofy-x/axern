package app

import (
	"bytes"
	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunInitializesOnlyOCI(t *testing.T) {
	if root := os.Getenv("IMAGEMGR_OCI_STARTUP_TEST"); root != "" {
		if err := Run([]string{"-root=" + root, "-http_sock="}); err == nil || !strings.Contains(err.Error(), "serve http: empty socket path") {
			t.Fatalf("OCI initialization failed: %v", err)
		}
		return
	}
	root := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRunInitializesOnlyOCI$")
	cmd.Env = append(os.Environ(), "IMAGEMGR_OCI_STARTUP_TEST="+root)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("startup subprocess: %v\n%s", err, output)
	}
	if _, err := os.Stat(filepath.Join(root, "oci", "metadata.db")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"daemon_configs", "daemons", "chunk_db", "image_metas"} {
		if _, err := os.Lstat(filepath.Join(root, name)); !os.IsNotExist(err) {
			t.Fatalf("OCI startup created retired state %s: %v", name, err)
		}
	}
}

func TestRetiredStartupFlagIsRejected(t *testing.T) {
	if err := Run([]string{"-enable_nydus=false"}); err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
		t.Fatalf("retired mode flag was accepted: %v", err)
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
			if err := Run([]string{"-root=" + root}); err == nil || !strings.Contains(err.Error(), "retired") {
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
