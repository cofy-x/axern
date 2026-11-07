package hostlinux

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNodeMemoryCapacityIdentitySurvivesEmptySiblingRecreation(t *testing.T) {
	root := t.TempDir()
	sandbox := filepath.Join(root, "sandbox")
	for _, name := range []string{"sandbox", "internal", "conformance"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	before, err := nodeMemoryCapacityIdentity("boot-a", "mount-a", root, sandbox)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"internal", "conformance"} {
		path := filepath.Join(root, name)
		// Keep the old inode allocated so the replacement cannot reuse it.
		if err := os.Rename(path, path+"-old"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		after, err := nodeMemoryCapacityIdentity("boot-a", "mount-a", root, sandbox)
		if err != nil || after != before {
			t.Fatalf("empty %s recreation changed user capacity identity: %q, %v", name, after, err)
		}
	}
}

func TestNodeMemoryCapacityIdentityFencesUserDomain(t *testing.T) {
	for _, change := range []string{"boot", "mount", "root", "sandbox", "missing root", "missing sandbox"} {
		t.Run(change, func(t *testing.T) {
			root := filepath.Join(t.TempDir(), "node")
			sandbox := filepath.Join(root, "sandbox")
			if err := os.MkdirAll(sandbox, 0o700); err != nil {
				t.Fatal(err)
			}
			boot, mount := "boot-a", "mount-a"
			before, err := nodeMemoryCapacityIdentity(boot, mount, root, sandbox)
			if err != nil {
				t.Fatal(err)
			}
			switch change {
			case "boot":
				boot = "boot-b"
			case "mount":
				mount = "mount-b"
			default:
				path := root
				if change == "sandbox" || change == "missing sandbox" {
					path = sandbox
				}
				if err := os.Rename(path, path+"-old"); err != nil {
					t.Fatal(err)
				}
				if change == "root" || change == "sandbox" {
					if err := os.MkdirAll(sandbox, 0o700); err != nil {
						t.Fatal(err)
					}
				}
			}
			after, err := nodeMemoryCapacityIdentity(boot, mount, root, sandbox)
			if change == "missing root" || change == "missing sandbox" {
				if err == nil {
					t.Fatalf("accepted %s", change)
				}
			} else if err != nil || before == after {
				t.Fatalf("%s replacement did not change identity: %q, %v", change, after, err)
			}
		})
	}
}
