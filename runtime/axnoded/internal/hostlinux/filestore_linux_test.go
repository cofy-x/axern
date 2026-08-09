//go:build linux

package hostlinux

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestDirectoryIdentityRejectsSymlinkAndChangesAfterReplacement(t *testing.T) {
	parent := t.TempDir()
	directory := filepath.Join(parent, "runsc")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	first, err := DirectoryIdentity(directory)
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(parent, "runsc-link")
	if err := os.Symlink(directory, link); err != nil {
		t.Fatal(err)
	}
	if _, err := DirectoryIdentity(link); err == nil {
		t.Fatal("symlink was accepted as durable backing directory identity")
	}
	if err := os.Remove(directory); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	second, err := DirectoryIdentity(directory)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("directory replacement preserved identity %q", first)
	}
}

func TestCanonicalMountIdentityIsBoundedAndUnambiguous(t *testing.T) {
	pre := []string{"42", "1", "8:1", `/root\040with\040space`, `/mnt\040with\040space`, "rw,relatime", "shared:7"}
	post := []string{"xfs", `/dev/mapper/data\040volume`, "rw,attr2,inode64,prjquota"}
	identity, err := canonicalMountIdentity(pre, post)
	if err != nil {
		t.Fatal(err)
	}
	if len(identity) != len("mount:v2:42:sha256:")+64 {
		t.Fatalf("mount identity = %q, want bounded canonical digest", identity)
	}
	changed := append([]string(nil), post...)
	changed[1] = `/dev/mapper/other\040volume`
	other, err := canonicalMountIdentity(pre, changed)
	if err != nil {
		t.Fatal(err)
	}
	if other == identity {
		t.Fatal("different mount source produced the same mount identity")
	}
	remounted := append([]string(nil), pre...)
	remounted[5] = "ro,relatime"
	readOnlyIdentity, err := canonicalMountIdentity(remounted, post)
	if err != nil {
		t.Fatal(err)
	}
	if readOnlyIdentity == identity {
		t.Fatal("read-only remount preserved the mount identity")
	}
	withoutQuota := append([]string(nil), post...)
	withoutQuota[2] = "rw,attr2,inode64"
	quotaIdentity, err := canonicalMountIdentity(pre, withoutQuota)
	if err != nil {
		t.Fatal(err)
	}
	if quotaIdentity == identity {
		t.Fatal("project-quota option change preserved the mount identity")
	}
}

func TestCanonicalMountIdentityIgnoresNonSemanticOptionOrdering(t *testing.T) {
	left, err := canonicalMountIdentity(
		[]string{"42", "1", "8:1", "/", "/mnt", "rw,relatime", "master:2", "shared:7"},
		[]string{"xfs", "/dev/data", "rw,inode64,prjquota"},
	)
	if err != nil {
		t.Fatal(err)
	}
	right, err := canonicalMountIdentity(
		[]string{"42", "1", "8:1", "/", "/mnt", "relatime,rw", "shared:7", "master:2"},
		[]string{"xfs", "/dev/data", "prjquota,rw,inode64"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if left != right {
		t.Fatalf("mount option ordering changed identity: %q != %q", left, right)
	}
}

func TestPrepareFilestoreRejectsUnsafeOverlayOptionPath(t *testing.T) {
	for _, path := range []string{"/tmp/filestore,other", "/tmp/filestore:other", "/tmp/filestore\\other", "/tmp/filestore\nother"} {
		if err := PrepareFilestore(path, "existing", "", 0, 0); err == nil {
			t.Fatalf("PrepareFilestore(%q) accepted an unsafe option path", path)
		}
	}
}

func TestQuotaBoundaryErrorAcceptsKernelQuotaResults(t *testing.T) {
	for _, err := range []error{unix.EDQUOT, unix.ENOSPC, errors.Join(errors.New("write failed"), unix.EDQUOT)} {
		if !quotaBoundaryError(err) {
			t.Fatalf("quotaBoundaryError(%v) = false, want true", err)
		}
	}
	if quotaBoundaryError(unix.EIO) {
		t.Fatal("quotaBoundaryError(EIO) = true, want false")
	}
}
