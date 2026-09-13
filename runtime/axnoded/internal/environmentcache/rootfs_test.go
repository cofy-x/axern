package environmentcache

import (
	"testing"

	api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

type malformedMountResultMounter struct {
	umounts int
	result  *MountResult
}

func (m *malformedMountResultMounter) Resolve(cfg RootfsConfig) (RootfsConfig, error) {
	return cfg, nil
}

func (m *malformedMountResultMounter) Mount(RootfsConfig) (*MountResult, error) {
	if m.result != nil {
		return m.result, nil
	}
	return &MountResult{
		Path: "/mounted/rootfs",
		ImmutableMount: &api.ImmutableRootfsMount{
			Identity: "derived-v1", EffectiveRoot: "/mounted/rootfs", Filesystem: "overlay",
			LowerDirs: []string{"/mounted/lower"}, Readonly: true,
		},
	}, nil
}

func (m *malformedMountResultMounter) Umount(RootfsConfig) error {
	m.umounts++
	return nil
}

func (*malformedMountResultMounter) Reconcile([]string) error { return nil }

func TestNewRootFSLocalMountLifecycle(t *testing.T) {
	cleanupCalls := 0
	rootfs, err := NewRootFS(
		RootfsConfig{SrcType: api.RootfsSrcType_LOCAL, Path: t.TempDir()},
		&defaultMounter{},
		func() { cleanupCalls++ },
	)
	if err != nil {
		t.Fatalf("NewRootFS() error = %v", err)
	}
	if rootfs.Path() == "" {
		t.Fatal("expected local rootfs path to be populated")
	}

	if err := rootfs.IncActiveRef(); err != nil {
		t.Fatalf("IncActiveRef() error = %v", err)
	}
	if !rootfs.ReleaseActiveRef() {
		t.Fatal("expected active release to clean up local rootfs")
	}
	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", cleanupCalls)
	}
}

func TestNewRootFSRollsBackMalformedSourceDescriptor(t *testing.T) {
	mounter := &malformedMountResultMounter{}
	_, err := NewRootFS(RootfsConfig{SrcType: api.RootfsSrcType_LOCAL, Path: "/source"}, mounter, nil)
	if err == nil {
		t.Fatal("expected malformed immutable mount descriptor to fail")
	}
	if mounter.umounts != 1 {
		t.Fatalf("umount calls = %d, want 1", mounter.umounts)
	}
}

func TestNewRootFSRollsBackMismatchedSourceLease(t *testing.T) {
	mounter := &malformedMountResultMounter{result: &MountResult{
		Path: "/mounted/rootfs",
		ImmutableMount: &api.ImmutableRootfsMount{
			Identity:      "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			EffectiveRoot: "/mounted/rootfs", Filesystem: "overlay", LowerDirs: []string{"/mounted/lower"},
			Readonly: true, LeaseID: "different-lease",
		},
	}}
	_, err := NewRootFS(RootfsConfig{SrcType: api.RootfsSrcType_IMAGE, LeaseID: "requested-lease"}, mounter, nil)
	if err == nil {
		t.Fatal("expected mismatched source lease to fail")
	}
	if mounter.umounts != 1 {
		t.Fatalf("umount calls = %d, want 1", mounter.umounts)
	}
}

func TestRootFSRefcountAndCleanup(t *testing.T) {
	mock := &mockMounter{}
	cleanupCalls := 0
	rootfs, err := NewRootFS(
		RootfsConfig{SrcType: api.RootfsSrcType_LOCAL, Path: "/shared"},
		mock,
		func() { cleanupCalls++ },
	)
	if err != nil {
		t.Fatalf("NewRootFS() error = %v", err)
	}

	if err := rootfs.IncActiveRef(); err != nil {
		t.Fatalf("IncActiveRef(first) error = %v", err)
	}
	if err := rootfs.IncActiveRef(); err != nil {
		t.Fatalf("IncActiveRef(second) error = %v", err)
	}
	if rootfs.ReleaseActiveRef() {
		t.Fatal("expected first release to keep shared rootfs alive")
	}
	if mock.UmountCount() != 0 {
		t.Fatalf("unexpected umount count after first release: %d", mock.UmountCount())
	}
	if !rootfs.ReleaseActiveRef() {
		t.Fatal("expected second release to clean up rootfs")
	}
	if mock.UmountCount() != 1 {
		t.Fatalf("umount count = %d, want 1", mock.UmountCount())
	}
	if cleanupCalls != 1 {
		t.Fatalf("cleanup calls = %d, want 1", cleanupCalls)
	}
}

func TestRootFSRetainedRefTransitions(t *testing.T) {
	rootfs, err := NewRootFS(
		RootfsConfig{SrcType: api.RootfsSrcType_LOCAL, Path: "/retained"},
		&mockMounter{},
		nil,
	)
	if err != nil {
		t.Fatalf("NewRootFS() error = %v", err)
	}

	if err := rootfs.IncActiveRef(); err != nil {
		t.Fatalf("IncActiveRef() error = %v", err)
	}
	rootfs.MoveActiveToRetained()
	if got := rootfs.RetainedRefCount(); got != 1 {
		t.Fatalf("RetainedRefCount() = %d, want 1", got)
	}
	if err := rootfs.MoveRetainedToActive(); err != nil {
		t.Fatalf("MoveRetainedToActive() error = %v", err)
	}
	if got := rootfs.RetainedRefCount(); got != 0 {
		t.Fatalf("RetainedRefCount() after resume = %d, want 0", got)
	}
}

func TestDefaultMounterDelegatesRemoteSources(t *testing.T) {
	client := &mockImageManagerClient{
		ociMountPath: "/mnt/oci/rootfs",
		ociEnv:       []string{"OCI_ENV=1"},
		ociConfig:    &ImageConfig{Entrypoint: []string{"/entrypoint"}, Cmd: []string{"serve"}, WorkingDir: "/app"},
	}
	lm := NewEnvironmentCache(&defaultMounter{client: client})

	imgRuntime, err := addTestEnvironmentCache(lm, &api.EnvironmentTemplate{
		ID: "img-runtime",
		Rootfs: &api.RootfsConfig{
			Type: api.RootfsSrcType_IMAGE,
			Source: &api.RootfsConfig_ImageUrl{
				ImageUrl: "docker.io/library/alpine:latest",
			},
		},
	})
	if err != nil {
		t.Fatalf("IMAGE PrepareEnvironment failed: %v", err)
	}
	if got := imgRuntime.RootFS.Path(); got != client.ociMountPath {
		t.Fatalf("IMAGE mount path = %q, want %q", got, client.ociMountPath)
	}
	if len(imgRuntime.RootFS.Env()) != 1 || imgRuntime.RootFS.Env()[0] != "OCI_ENV=1" {
		t.Fatalf("IMAGE env = %v, want %v", imgRuntime.RootFS.Env(), client.ociEnv)
	}
	if got := imgRuntime.RootFS.DefaultCommand(); len(got) != 2 || got[0] != "/entrypoint" || got[1] != "serve" {
		t.Fatalf("IMAGE default command = %v, want entrypoint plus cmd", got)
	}
	if got := imgRuntime.RootFS.WorkingDir(); got != "/app" {
		t.Fatalf("IMAGE working dir = %q, want /app", got)
	}

	if client.ociMounts != 1 {
		t.Fatalf("unexpected OCI mount calls: %d", client.ociMounts)
	}
}

func TestRootfsConfigRejectsUnspecifiedSource(t *testing.T) {
	_, err := RootfsConfigFromEnvironmentTemplate(&api.EnvironmentTemplate{
		Rootfs: &api.RootfsConfig{Type: api.RootfsSrcType_UNSPECIFIED},
	})
	if err == nil {
		t.Fatal("unspecified rootfs source must be rejected")
	}
}
