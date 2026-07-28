package langruntime

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

// mockMounter is a test ImageMounter that returns a fake path and tracks calls.
type mockMounter struct {
	mu          sync.Mutex
	mountCount  int
	umountCount int
	mountDelay  time.Duration
	mountErr    error
	resolveFunc func(RootfsConfig) (RootfsConfig, error)
}

func (m *mockMounter) Resolve(cfg RootfsConfig) (RootfsConfig, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(cfg)
	}
	return cfg, nil
}

func (m *mockMounter) Mount(cfg RootfsConfig) (*MountResult, error) {
	if m.mountDelay > 0 {
		time.Sleep(m.mountDelay)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.mountErr != nil {
		return nil, m.mountErr
	}
	m.mountCount++
	return &MountResult{Path: fmt.Sprintf("/fake/rootfs/%s/%d", cfg.Path, m.mountCount)}, nil
}

func (m *mockMounter) Umount(cfg RootfsConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.umountCount++
	return nil
}

func (m *mockMounter) Reconcile([]string) error { return nil }

func (m *mockMounter) MountCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.mountCount
}

func (m *mockMounter) UmountCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.umountCount
}

type mockImageManagerClient struct {
	ociMountPath string
	ociEnv       []string
	ociConfig    *ImageConfig
	ossMountPath string
	ossEnv       []string
	ociMounts    int
	ossMounts    int
	reconciled   []string
}

func (m *mockImageManagerClient) ResolveOCIImageCacheKey(imageURL string) (string, error) {
	return imageURL, nil
}

func (m *mockImageManagerClient) MountOCI(req *ociMountRequest) (*imageManagerMountInfo, error) {
	m.ociMounts++
	return &imageManagerMountInfo{MountPath: m.ociMountPath, Env: append([]string(nil), m.ociEnv...), ImageConfig: cloneImageConfig(m.ociConfig)}, nil
}

func (m *mockImageManagerClient) UmountOCI(req *ociUmountRequest) error {
	return nil
}

func (m *mockImageManagerClient) MountOSS(req *ossMountRequest) (*imageManagerMountInfo, error) {
	m.ossMounts++
	return &imageManagerMountInfo{MountPath: m.ossMountPath, Env: append([]string(nil), m.ossEnv...)}, nil
}

func (m *mockImageManagerClient) UmountOSS(req *ossUmountRequest) error {
	return nil
}

func (m *mockImageManagerClient) ReconcileMountLeases(req *reconcileMountLeasesRequest) error {
	m.reconciled = append([]string(nil), req.LeaseIDs...)
	return nil
}

func TestRootfsLeaseIDStableAndSeparatesCredentialContexts(t *testing.T) {
	base := RootfsConfig{
		SrcType:          api.RootfsSrcType_IMAGE,
		ImageUrl:         "registry.example/image:latest",
		ImageCacheKey:    "registry.example/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		DockerConfigJSON: `{"auths":{"registry.example":{"auth":"first"}}}`,
	}
	changedCredential := base
	changedCredential.DockerConfigJSON = `{"auths":{"registry.example":{"auth":"second"}}}`
	if got, want := rootfsLeaseID(base), rootfsLeaseID(base); got != want {
		t.Fatalf("lease ID is unstable: got %q want %q", got, want)
	}
	if rootfsLeaseID(changedCredential) == rootfsLeaseID(base) {
		t.Fatal("different credential contexts produced the same lease ID")
	}
	changedResource := base
	changedResource.ImageCacheKey += "-different"
	if rootfsLeaseID(changedResource) == rootfsLeaseID(base) {
		t.Fatal("different image resources produced the same lease ID")
	}
}

func TestRootfsConfigStringRedactsCredentials(t *testing.T) {
	cfg := RootfsConfig{
		SrcType:          api.RootfsSrcType_S3,
		Endpoint:         "oss.example",
		Bucket:           "bucket",
		Object:           "rootfs.ext4",
		AccessKeyID:      "sensitive-access-key",
		AccessKeySecret:  "sensitive-access-secret",
		DockerConfigJSON: "sensitive-registry-auth",
	}
	got := cfg.String()
	for _, secret := range []string{cfg.AccessKeyID, cfg.AccessKeySecret, cfg.DockerConfigJSON} {
		if strings.Contains(got, secret) {
			t.Fatalf("RootfsConfig.String() exposed credential %q: %s", secret, got)
		}
	}
	if !strings.Contains(got, "has_object_credentials:true") || !strings.Contains(got, "has_registry_auth:true") {
		t.Fatalf("RootfsConfig.String() omitted safe credential presence flags: %s", got)
	}
}

func TestDefaultMounterReconcileSortsLeaseIDs(t *testing.T) {
	client := &mockImageManagerClient{}
	mounter := &defaultMounter{client: client}
	if err := mounter.Reconcile([]string{"lease-b", "lease-a"}); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(client.reconciled); got != "[lease-a lease-b]" {
		t.Fatalf("reconciled leases = %s", got)
	}
}

type blockingLeaseMounter struct {
	mountStarted chan struct{}
	allowMount   chan struct{}
}

func (m *blockingLeaseMounter) Resolve(cfg RootfsConfig) (RootfsConfig, error) { return cfg, nil }
func (m *blockingLeaseMounter) Mount(RootfsConfig) (*MountResult, error) {
	close(m.mountStarted)
	<-m.allowMount
	return &MountResult{Path: "/mounted"}, nil
}
func (m *blockingLeaseMounter) Umount(RootfsConfig) error { return nil }
func (m *blockingLeaseMounter) Reconcile([]string) error  { return nil }

func TestMountLeaseReconcileWaitsForInflightAcquire(t *testing.T) {
	mounter := &blockingLeaseMounter{mountStarted: make(chan struct{}), allowMount: make(chan struct{})}
	manager := NewLanguageRuntimeManager(mounter)
	mountDone := make(chan error, 1)
	go func() {
		_, err := manager.GetRootfs(RootfsConfig{SrcType: api.RootfsSrcType_IMAGE, ImageUrl: "image", LeaseID: "lease"})
		mountDone <- err
	}()
	<-mounter.mountStarted

	reconcileDone := make(chan error, 1)
	go func() { reconcileDone <- manager.ReconcileMountLeases() }()
	select {
	case err := <-reconcileDone:
		t.Fatalf("reconciliation passed inflight mount: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(mounter.allowMount)
	if err := <-mountDone; err != nil {
		t.Fatal(err)
	}
	if err := <-reconcileDone; err != nil {
		t.Fatal(err)
	}
}

func newTestFR(id, path string) *api.RuntimeTemplate {
	return &api.RuntimeTemplate{
		ID: id,
		Rootfs: &api.RootfsConfig{
			Type: api.RootfsSrcType_LOCAL,
			Source: &api.RootfsConfig_Path{
				Path: path,
			},
		},
		Sandbox: "runsc",
		Command: []string{"/bin/sh"},
	}
}

func addTestLangRuntime(lm *LangRTManager, fr *api.RuntimeTemplate, temporary bool) (*LanguageRuntime, error) {
	cfg, err := RootfsConfigFromRuntimeTemplate(fr)
	if err != nil {
		return nil, err
	}
	result, err := lm.AddLangRuntime(context.Background(), fr, cfg, temporary)
	return result.Runtime, err
}

func addTestLangRuntimeWithState(lm *LangRTManager, fr *api.RuntimeTemplate, temporary bool) (*LanguageRuntime, bool, error) {
	cfg, err := RootfsConfigFromRuntimeTemplate(fr)
	if err != nil {
		return nil, false, err
	}
	result, err := lm.AddLangRuntime(context.Background(), fr, cfg, temporary)
	return result.Runtime, result.Created, err
}
