package allocation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

type imageMountTestMounter struct {
	imagePaths map[string]string
	mountErr   error
	mountErrs  map[string]error
	umounts    []langrtmanager.RootfsConfig
	reconciled []string
}

func (m *imageMountTestMounter) Resolve(cfg langrtmanager.RootfsConfig) (langrtmanager.RootfsConfig, error) {
	if cfg.SrcType == runtime.RootfsSrcType_IMAGE {
		cfg.LeaseID = "test-lease:" + cfg.ImageUrl
	}
	return cfg, nil
}

func (m *imageMountTestMounter) Mount(cfg langrtmanager.RootfsConfig) (*langrtmanager.MountResult, error) {
	if m.mountErr != nil {
		return nil, m.mountErr
	}
	if err := m.mountErrs[cfg.ImageUrl]; err != nil {
		return nil, err
	}
	switch cfg.SrcType {
	case runtime.RootfsSrcType_LOCAL:
		mount, err := langrtmanager.DescribeLocalRootfs(cfg.Path)
		return &langrtmanager.MountResult{Path: cfg.Path, ImmutableMount: mount}, err
	case runtime.RootfsSrcType_IMAGE:
		path := m.imagePaths[cfg.ImageUrl]
		if err := os.MkdirAll(path, 0755); err != nil {
			return nil, err
		}
		mount, err := langrtmanager.DescribeLocalRootfs(path)
		if mount != nil {
			mount.LeaseID = cfg.LeaseID
		}
		return &langrtmanager.MountResult{Path: path, ImmutableMount: mount}, err
	default:
		return nil, nil
	}
}

func (m *imageMountTestMounter) Umount(cfg langrtmanager.RootfsConfig) error {
	m.umounts = append(m.umounts, cfg)
	return nil
}

func (m *imageMountTestMounter) Reconcile(leaseIDs []string) error {
	m.reconciled = append([]string(nil), leaseIDs...)
	return nil
}

func TestStartResolvesImageMountIntoReadonlyBindMount(t *testing.T) {
	rootfsDir := t.TempDir()
	imageDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(imageDir, "bin"), 0755); err != nil {
		t.Fatalf("MkdirAll(image bin) error = %v", err)
	}

	handler := &runtimeSpyHandler{name: "runsc", capabilities: contract.RuntimeCapabilities{CanExecDirect: true}}
	tc := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": handler})
	mounter := &imageMountTestMounter{imagePaths: map[string]string{
		"example.com/axern/codex-tool:latest": imageDir,
	}}
	tc.lrtManager = langrtmanager.NewLanguageRuntimeManager(mounter)
	tc.controller.lrtManager = tc.lrtManager

	resp, err := tc.controller.Start(context.Background(), &runtime.StartRequest{
		ContainerID: "alloc-image-mount",
		RuntimeTemplate: &runtime.RuntimeTemplate{
			ID:      "task-runtime",
			Sandbox: "runsc",
			Rootfs: &runtime.RootfsConfig{
				Type:   runtime.RootfsSrcType_LOCAL,
				Source: &runtime.RootfsConfig_Path{Path: rootfsDir},
			},
			Command: []string{"/bin/sh", "-lc", "sleep 3600"},
		},
		ImageMounts: []*runtime.ImageMount{{
			Image:  "example.com/axern/codex-tool:latest",
			Target: "/opt/axern/tools/codex",
		}},
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if resp.GetID() != "alloc-image-mount" {
		t.Fatalf("response id = %q, want alloc-image-mount", resp.GetID())
	}
	mounts := handler.lastRequest.GetMounts()
	if len(mounts) != 1 {
		t.Fatalf("mounts = %#v, want one image mount", mounts)
	}
	got := mounts[0]
	if got.GetType() != "bind" || got.GetSource() != imageDir || got.GetTarget() != "/opt/axern/tools/codex" {
		t.Fatalf("mount = %#v, want readonly image bind mount", got)
	}
	if len(got.GetOptions()) != 2 || got.GetOptions()[0] != "rbind" || got.GetOptions()[1] != "ro" {
		t.Fatalf("mount options = %#v, want rbind,ro", got.GetOptions())
	}
	if _, err := tc.controller.Delete(context.Background(), &runtime.DeleteRequest{ID: "alloc-image-mount"}); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	foundImageUmount := false
	for _, cfg := range mounter.umounts {
		if cfg.SrcType == runtime.RootfsSrcType_IMAGE && cfg.ImageUrl == "example.com/axern/codex-tool:latest" {
			foundImageUmount = true
		}
	}
	if !foundImageUmount {
		t.Fatalf("umounts = %#v, want image mount rootfs release", mounter.umounts)
	}
}

func TestStartReleasesImageMountWhenRuntimeCreateFails(t *testing.T) {
	rootfsDir := t.TempDir()
	imageDir := t.TempDir()
	handler := &runtimeSpyHandler{
		name:         "runsc",
		capabilities: contract.RuntimeCapabilities{CanExecDirect: true},
		createError:  errors.New("create failed"),
	}
	tc := newTestAllocationController(t, map[string]contract.RuntimeHandler{"runsc": handler})
	mounter := &imageMountTestMounter{imagePaths: map[string]string{
		"example.com/axern/tool:latest": imageDir,
	}}
	tc.lrtManager = langrtmanager.NewLanguageRuntimeManager(mounter)
	tc.controller.lrtManager = tc.lrtManager

	_, err := tc.controller.Start(context.Background(), &runtime.StartRequest{
		ContainerID: "alloc-image-mount-fail",
		RuntimeTemplate: &runtime.RuntimeTemplate{
			ID:      "task-runtime",
			Sandbox: "runsc",
			Rootfs: &runtime.RootfsConfig{
				Type:   runtime.RootfsSrcType_LOCAL,
				Source: &runtime.RootfsConfig_Path{Path: rootfsDir},
			},
			Command: []string{"/bin/sh", "-lc", "sleep 3600"},
		},
		ImageMounts: []*runtime.ImageMount{{
			Image:  "example.com/axern/tool:latest",
			Target: "/opt/axern/tools/tool",
		}},
	})
	if err == nil {
		t.Fatal("Start() error = nil, want runtime create error")
	}
	foundImageUmount := false
	for _, cfg := range mounter.umounts {
		if cfg.SrcType == runtime.RootfsSrcType_IMAGE && cfg.ImageUrl == "example.com/axern/tool:latest" {
			foundImageUmount = true
		}
	}
	if !foundImageUmount {
		t.Fatalf("umounts = %#v, want image mount release after create failure", mounter.umounts)
	}
}

func TestValidateImageMountTargetsRejectsProtectedAndOverlappingTargets(t *testing.T) {
	tests := []struct {
		name    string
		request *runtime.StartRequest
	}{
		{
			name: "protected",
			request: &runtime.StartRequest{ImageMounts: []*runtime.ImageMount{{
				Image: "image", Target: "/usr",
			}}},
		},
		{
			name: "overlapping dynamic mount",
			request: &runtime.StartRequest{
				ImageMounts: []*runtime.ImageMount{{Image: "image", Target: "/opt/axern/tools"}},
				Mounts:      []*runtime.Mount{{Target: "/opt/axern/tools/workspace"}},
			},
		},
		{
			name: "overlapping secret file mount",
			request: &runtime.StartRequest{
				ImageMounts: []*runtime.ImageMount{{Image: "image", Target: "/opt/axern/tools"}},
				Mounts:      []*runtime.Mount{{Target: "/opt/axern/tools/secret/token"}},
			},
		},
		{
			name: "overlapping image mounts",
			request: &runtime.StartRequest{ImageMounts: []*runtime.ImageMount{
				{Image: "image-a", Target: "/opt/axern/tools"},
				{Image: "image-b", Target: "/opt/axern/tools/codex"},
			}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := validateImageMountTargets(tc.request); err == nil {
				t.Fatal("validateImageMountTargets() error = nil, want error")
			}
		})
	}
}
