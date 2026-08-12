package api

import (
	"fmt"
	"strings"
	"testing"
)

func TestImmutableMountDescriptorIsStableAndIdentitySensitive(t *testing.T) {
	first, err := immutableMountDescriptor("/mnt/image", "lease-1", "overlay", "mount-1", []string{"/layers/top", "/layers/base"}, []string{"xfs", "erofs"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := immutableMountDescriptor("/mnt/image", "lease-1", "overlay", "mount-1", []string{"/layers/top", "/layers/base"}, []string{"erofs", "xfs"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity != second.Identity {
		t.Fatalf("stable descriptor identity differs: %q != %q", first.Identity, second.Identity)
	}
	changed, err := immutableMountDescriptor("/mnt/image", "lease-1", "overlay", "mount-2", []string{"/layers/top", "/layers/base"}, []string{"xfs", "erofs"})
	if err != nil {
		t.Fatal(err)
	}
	if first.Identity == changed.Identity {
		t.Fatal("different source identity produced the same immutable mount identity")
	}
	if !strings.HasPrefix(first.Identity, "sha256:") || len(first.Identity) != 71 {
		t.Fatalf("identity = %q, want canonical sha256", first.Identity)
	}
}

func TestImmutableMountDescriptorRejectsUnboundedOrMountUnsafeInput(t *testing.T) {
	tests := []struct {
		name       string
		mountPath  string
		filesystem string
		lowers     []string
		backing    []string
	}{
		{name: "non-canonical root", mountPath: "/mnt/../image", filesystem: "overlay", lowers: []string{"/layers/base"}},
		{name: "mount delimiter", mountPath: "/mnt/image", filesystem: "overlay", lowers: []string{"/layers/with:colon"}},
		{name: "duplicate lower", mountPath: "/mnt/image", filesystem: "overlay", lowers: []string{"/layers/base", "/layers/base"}},
		{name: "invalid filesystem", mountPath: "/mnt/image", filesystem: "not valid", lowers: []string{"/layers/base"}},
		{name: "non-canonical filesystem", mountPath: "/mnt/image", filesystem: "Overlay", lowers: []string{"/layers/base"}},
		{name: "padded lower", mountPath: "/mnt/image", filesystem: "overlay", lowers: []string{" /layers/base"}},
		{name: "duplicate backing", mountPath: "/mnt/image", filesystem: "overlay", lowers: []string{"/layers/base"}, backing: []string{"xfs", "xfs"}},
		{name: "non-canonical backing", mountPath: "/mnt/image", filesystem: "overlay", lowers: []string{"/layers/base"}, backing: []string{"XFS"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := immutableMountDescriptor(test.mountPath, "lease-1", test.filesystem, "mount-1", test.lowers, test.backing); err == nil {
				t.Fatal("expected invalid immutable mount descriptor")
			}
		})
	}

	tooMany := make([]string, maxImmutableLowerDirs+1)
	for index := range tooMany {
		tooMany[index] = fmt.Sprintf("/layers/%d", index)
	}
	if _, err := immutableMountDescriptor("/mnt/image", "lease-1", "overlay", "mount-1", tooMany, nil); err == nil {
		t.Fatal("expected bounded lower-dir rejection")
	}
	if _, err := immutableMountDescriptor("/mnt/image", " lease-1", "overlay", "mount-1", []string{"/layers/base"}, nil); err == nil {
		t.Fatal("expected non-canonical lease rejection")
	}
	if _, err := immutableMountDescriptor("/mnt/image", "lease-1", "overlay", " mount-1", []string{"/layers/base"}, nil); err == nil {
		t.Fatal("expected non-canonical resource identity rejection")
	}
}
