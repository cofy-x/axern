package startplan

import (
	"reflect"
	"testing"
	"unsafe"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
)

func TestBuildContainerRootfsPreservesLanguageRuntimeSettings(t *testing.T) {
	rootfsDir := t.TempDir()
	lrt := &langrtmanager.LanguageRuntime{
		Readonly: false,
		RootFS:   &langrtmanager.RootFS{},
	}
	setRootFSPath(t, lrt.RootFS, rootfsDir)

	rootfs := BuildContainerRootfs(lrt)
	if rootfs == nil {
		t.Fatal("BuildContainerRootfs() = nil")
	}
	if rootfs.Readonly {
		t.Fatalf("Readonly = true, want false from LanguageRuntime")
	}
	if rootfs.RootDir != rootfsDir {
		t.Fatalf("RootDir = %q, want %q", rootfs.RootDir, rootfsDir)
	}
}

func TestBuildContainerRootfsPreservesConfiguredReadonly(t *testing.T) {
	rootfsDir := t.TempDir()
	lrt := &langrtmanager.LanguageRuntime{
		Readonly: true,
		RootFS:   &langrtmanager.RootFS{},
	}
	setRootFSPath(t, lrt.RootFS, rootfsDir)

	rootfs := BuildContainerRootfs(lrt)
	if rootfs == nil {
		t.Fatal("BuildContainerRootfs() = nil")
	}
	if !rootfs.Readonly {
		t.Fatalf("Readonly = false, want true when runtime is configured readonly")
	}
}

func TestBuildCreateContainerRequestUsesImageDefaultsWhenCommandEmpty(t *testing.T) {
	lrt := &langrtmanager.LanguageRuntime{
		RootFS: &langrtmanager.RootFS{},
	}
	setRootFSImageConfig(t, lrt.RootFS, &langrtmanager.ImageConfig{
		Entrypoint: []string{"/usr/bin/supervisord"},
		Cmd:        []string{"-c", "/etc/supervisor/conf.d/supervisord.conf"},
		WorkingDir: "/home/axern",
	})

	req := &apipb.StartRequest{
		RuntimeTemplate: &apipb.RuntimeTemplate{Sandbox: "runsc"},
	}
	containerReq := BuildCreateContainerRequest(lrt, req, nil, nil, "")
	if got := containerReq.GetCommand(); !reflect.DeepEqual(got, []string{"/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"}) {
		t.Fatalf("command = %#v, want image entrypoint plus cmd", got)
	}
	if got := containerReq.GetCwd(); got != "/home/axern" {
		t.Fatalf("cwd = %q, want image working dir", got)
	}
}

func TestBuildCreateContainerRequestExplicitCommandOverridesImageDefaults(t *testing.T) {
	lrt := &langrtmanager.LanguageRuntime{
		RootFS: &langrtmanager.RootFS{},
	}
	setRootFSImageConfig(t, lrt.RootFS, &langrtmanager.ImageConfig{
		Cmd:        []string{"/image-default"},
		WorkingDir: "/image",
	})

	req := &apipb.StartRequest{
		RuntimeTemplate: &apipb.RuntimeTemplate{
			Sandbox: "runsc",
			Command: []string{"/bin/sh", "-lc", "sleep 60"},
			Cwd:     "/workspace",
		},
	}
	containerReq := BuildCreateContainerRequest(lrt, req, nil, nil, "")
	if got := containerReq.GetCommand(); !reflect.DeepEqual(got, []string{"/bin/sh", "-lc", "sleep 60"}) {
		t.Fatalf("command = %#v, want explicit request command", got)
	}
	if got := containerReq.GetCwd(); got != "/workspace" {
		t.Fatalf("cwd = %q, want explicit request cwd", got)
	}
}

func setRootFSPath(t *testing.T, rf *langrtmanager.RootFS, path string) {
	t.Helper()
	field := reflect.ValueOf(rf).Elem().FieldByName("path")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().SetString(path)
}

func setRootFSImageConfig(t *testing.T, rf *langrtmanager.RootFS, config *langrtmanager.ImageConfig) {
	t.Helper()
	field := reflect.ValueOf(rf).Elem().FieldByName("imageConfig")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(config))
}
