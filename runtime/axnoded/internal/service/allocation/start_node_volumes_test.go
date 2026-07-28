package allocation

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
)

func TestActiveAllocationIDsNormalizesForVolumeReconcile(t *testing.T) {
	got := activeAllocationIDs([]*container.Container{
		nil,
		{Metadata: nil},
		{Metadata: &runtime.ContainerMetadata{ID: " alloc-b "}},
		{Metadata: &runtime.ContainerMetadata{ID: ""}},
		{Metadata: &runtime.ContainerMetadata{ID: "alloc-a"}},
		{Metadata: &runtime.ContainerMetadata{ID: "alloc-b"}},
	})
	want := []string{"alloc-a", "alloc-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("activeAllocationIDs() = %#v, want %#v", got, want)
	}
}

func TestValidateMountTargetsForRootfsReadonlyAllowsExistingTargets(t *testing.T) {
	rootfsDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(rootfsDir, "var", "lib", "app"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	err := ValidateMountTargetsForRootfsReadonly(rootfsDir, true, []*runtime.Mount{{
		Type:   "bind",
		Source: "/host/data",
		Target: "/var/lib/app",
	}})
	if err != nil {
		t.Fatalf("ValidateMountTargetsForRootfsReadonly() error = %v", err)
	}
}

func TestValidateMountTargetsForRootfsReadonlyRejectsMissingTarget(t *testing.T) {
	rootfsDir := t.TempDir()
	err := ValidateMountTargetsForRootfsReadonly(rootfsDir, true, []*runtime.Mount{{
		Type:   "bind",
		Source: "/host/data",
		Target: "/var/lib/app",
	}})
	if err == nil {
		t.Fatal("expected readonly rootfs missing target error")
	}
	if got := err.Error(); got != `mount target "/var/lib/app" does not exist in readonly rootfs` {
		t.Fatalf("error = %q, want missing-target message", got)
	}
}

func TestValidateMountTargetsForRootfsReadonlySkipsWritableRootfs(t *testing.T) {
	rootfsDir := t.TempDir()
	err := ValidateMountTargetsForRootfsReadonly(rootfsDir, false, []*runtime.Mount{{
		Type:   "bind",
		Source: "/host/data",
		Target: "/var/lib/app",
	}})
	if err != nil {
		t.Fatalf("ValidateMountTargetsForRootfsReadonly() error = %v", err)
	}
}
