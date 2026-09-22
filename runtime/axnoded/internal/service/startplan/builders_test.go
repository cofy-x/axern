package startplan

import (
	"path/filepath"
	"reflect"
	"testing"
	"unsafe"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	environmentcache "github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

func TestTerminalEnvironmentPrecedenceAcrossStartAndTemplate(t *testing.T) {
	for _, level := range []string{"image", "environment", "run"} {
		t.Run(level, func(t *testing.T) {
			root := t.TempDir()
			rf := &environmentcache.RootFS{}
			setRootFSPath(t, rf, root)
			field := reflect.ValueOf(rf).Elem().FieldByName("env")
			reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf([]string{"TERM=image"}))
			lrt := &environmentcache.PreparedEnvironment{RootFS: rf}
			request := &apipb.StartRequest{Environment: &apipb.ResolvedEnvironment{}, Env: map[string]string{}}
			if level != "image" {
				lrt.Env = map[string]string{"TERM": "environment"}
				request.Environment.Env = lrt.Env
			}
			if level == "run" {
				request.Env["TERM"] = "run"
			}
			loader, err := oci.NewBundleLoader(filepath.Join(root, "bundles"))
			if err != nil {
				t.Fatal(err)
			}
			static := &apipb.CreateContainerRequest{Command: []string{"/bin/true"}, Envs: BuildStaticRuntimeEnv(lrt)}
			template, err := loader.PrepareBundleTemplate(oci.TemplateOptions{Request: static})
			if err != nil {
				t.Fatal(err)
			}
			for _, cached := range []bool{false, true} {
				create := &apipb.CreateContainerRequest{Command: []string{"/bin/true"}, Rootfs: &apipb.Rootfs{RootDir: root}, Envs: BuildStartEnv(lrt, request)}
				opts := oci.LoadOptions{ContainerID: "allocation", Request: create}
				if cached {
					opts.ContainerID = "allocation-template"
					create.Envs = BuildDynamicStartEnv(request)
					_, s, err := loader.MaterializeBundle(template, opts)
					if err != nil {
						t.Fatal(err)
					}
					if !containsEnv(s.Process.Env, "TERM="+level) {
						t.Fatalf("template env: %v", s.Process.Env)
					}
				} else {
					_, s, err := loader.Generate(opts)
					if err != nil {
						t.Fatal(err)
					}
					if !containsEnv(s.Process.Env, "TERM="+level) {
						t.Fatalf("direct env: %v", s.Process.Env)
					}
				}
			}
		})
	}
}

func containsEnv(env []string, want string) bool {
	for _, v := range env {
		if v == want {
			return true
		}
	}
	return false
}

func TestBuildContainerRootfsPreservesPreparedEnvironmentSettings(t *testing.T) {
	rootfsDir := t.TempDir()
	rootfsSource, err := environmentcache.NewRootFS(
		environmentcache.RootfsConfig{SrcType: apipb.RootfsSrcType_LOCAL, Path: rootfsDir},
		environmentcache.NewDefaultMounter(false, ""), nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	lrt := &environmentcache.PreparedEnvironment{
		Readonly: false,
		RootFS:   rootfsSource,
	}

	rootfs := BuildContainerRootfs(lrt)
	if rootfs == nil {
		t.Fatal("BuildContainerRootfs() = nil")
	}
	if rootfs.Readonly {
		t.Fatalf("Readonly = true, want false from PreparedEnvironment")
	}
	if rootfs.RootDir != rootfsDir {
		t.Fatalf("RootDir = %q, want %q", rootfs.RootDir, rootfsDir)
	}
	if rootfs.GetImmutableMount().GetEffectiveRoot() != rootfsDir || !rootfs.GetImmutableMount().GetReadonly() {
		t.Fatalf("ImmutableMount = %#v, want source-owned descriptor for %q", rootfs.GetImmutableMount(), rootfsDir)
	}
}

func TestBuildContainerRootfsPreservesConfiguredReadonly(t *testing.T) {
	rootfsDir := t.TempDir()
	lrt := &environmentcache.PreparedEnvironment{
		Readonly: true,
		RootFS:   &environmentcache.RootFS{},
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
	lrt := &environmentcache.PreparedEnvironment{
		RootFS: &environmentcache.RootFS{},
	}
	setRootFSImageConfig(t, lrt.RootFS, &environmentcache.ImageConfig{
		Entrypoint: []string{"/usr/bin/supervisord"},
		Cmd:        []string{"-c", "/etc/supervisor/conf.d/supervisord.conf"},
		WorkingDir: "/home/axern",
	})

	req := &apipb.StartRequest{
		Environment: &apipb.ResolvedEnvironment{},
	}
	containerReq := BuildCreateContainerRequest(lrt, req, nil)
	if got := containerReq.GetCommand(); !reflect.DeepEqual(got, []string{"/usr/bin/supervisord", "-c", "/etc/supervisor/conf.d/supervisord.conf"}) {
		t.Fatalf("command = %#v, want image entrypoint plus cmd", got)
	}
	if got := containerReq.GetCwd(); got != "/home/axern" {
		t.Fatalf("cwd = %q, want image working dir", got)
	}
}

func TestBuildCreateContainerRequestExplicitCommandOverridesImageDefaults(t *testing.T) {
	lrt := &environmentcache.PreparedEnvironment{
		RootFS: &environmentcache.RootFS{},
	}
	setRootFSImageConfig(t, lrt.RootFS, &environmentcache.ImageConfig{
		Cmd:        []string{"/image-default"},
		WorkingDir: "/image",
	})

	req := &apipb.StartRequest{
		Environment: &apipb.ResolvedEnvironment{
			Argv: []string{"/bin/sh", "-lc", "sleep 60"},
			Cwd:  "/workspace",
		},
	}
	containerReq := BuildCreateContainerRequest(lrt, req, nil)
	if got := containerReq.GetCommand(); !reflect.DeepEqual(got, []string{"/bin/sh", "-lc", "sleep 60"}) {
		t.Fatalf("command = %#v, want explicit request command", got)
	}
	if got := containerReq.GetCwd(); got != "/workspace" {
		t.Fatalf("cwd = %q, want explicit request cwd", got)
	}
}

func setRootFSPath(t *testing.T, rf *environmentcache.RootFS, path string) {
	t.Helper()
	field := reflect.ValueOf(rf).Elem().FieldByName("path")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().SetString(path)
}

func setRootFSImageConfig(t *testing.T, rf *environmentcache.RootFS, config *environmentcache.ImageConfig) {
	t.Helper()
	field := reflect.ValueOf(rf).Elem().FieldByName("imageConfig")
	reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Set(reflect.ValueOf(config))
}
