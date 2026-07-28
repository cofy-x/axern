package storage

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

func TestManagerPublishesLocalVolumeAndPersistsState(t *testing.T) {
	root := t.TempDir()
	localRoot := filepath.Join(root, "local")
	manager, err := NewDefaultManager(root, localRoot)
	if err != nil {
		t.Fatalf("NewDefaultManager() error = %v", err)
	}

	published, err := manager.Publish(context.Background(), "alloc-1", "runsc", localVolume())
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if got, want := published.GetHostPath(), filepath.Join(localRoot, "claim-1"); got != want {
		t.Fatalf("HostPath = %q, want %q", got, want)
	}
	if !reflect.DeepEqual(published.GetOptions(), []string{"rbind", "nodev", "ro"}) {
		t.Fatalf("Options = %#v, want normalized readonly options", published.GetOptions())
	}

	reloaded, err := NewDefaultManager(root, localRoot)
	if err != nil {
		t.Fatalf("NewDefaultManager() reload error = %v", err)
	}
	got, ok := reloaded.Get("alloc-1", "binding-1")
	if !ok {
		t.Fatal("Get() after reload not found")
	}
	if got.GetHostPath() != published.GetHostPath() {
		t.Fatalf("reloaded HostPath = %q, want %q", got.GetHostPath(), published.GetHostPath())
	}
}

func TestManagerDeletesUnpublishedLocalVolumeIdempotently(t *testing.T) {
	root := t.TempDir()
	localRoot := filepath.Join(root, "local")
	manager, err := NewDefaultManager(root, localRoot)
	if err != nil {
		t.Fatal(err)
	}
	published, err := manager.Publish(context.Background(), "alloc-1", "runsc", localVolume())
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Delete(context.Background(), "claim-1", storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL, "claim-1"); err == nil || !strings.Contains(err.Error(), "still published") {
		t.Fatalf("Delete() published error = %v", err)
	}
	if _, err := manager.Unpublish(context.Background(), "alloc-1", "binding-1"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Delete(context.Background(), "claim-1", storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL, "claim-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(published.GetHostPath()); !os.IsNotExist(err) {
		t.Fatalf("deleted path stat error = %v", err)
	}
	if err := manager.Delete(context.Background(), "claim-1", storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL, "claim-1"); err != nil {
		t.Fatalf("idempotent Delete() error = %v", err)
	}
}

func TestLocalProviderDeleteRejectsUnsafeIdentityAndSymlink(t *testing.T) {
	root := t.TempDir()
	provider := NewLocalProvider(root)
	for _, tc := range []struct{ claim, handle string }{
		{"..", ".."}, {"claim/1", "claim/1"}, {"claim-1", "claim-2"}, {"", ""},
	} {
		if err := provider.Delete(context.Background(), tc.claim, tc.handle); err == nil {
			t.Fatalf("Delete(%q, %q) succeeded", tc.claim, tc.handle)
		}
	}
	target := filepath.Join(root, "outside")
	if err := os.Mkdir(target, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "claim-1")); err != nil {
		t.Fatal(err)
	}
	if err := provider.Delete(context.Background(), "claim-1", "claim-1"); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink Delete() error = %v", err)
	}
}

func TestLocalProviderDeleteRejectsSymlinkRoot(t *testing.T) {
	parent := t.TempDir()
	outside := filepath.Join(parent, "outside")
	if err := os.Mkdir(outside, 0o750); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "local-link")
	if err := os.Symlink(outside, root); err != nil {
		t.Fatal(err)
	}
	if err := NewLocalProvider(root).Delete(context.Background(), "claim-1", "claim-1"); err == nil || !strings.Contains(err.Error(), "root") || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlink root Delete() error = %v", err)
	}
}

func TestManagerRejectsInvalidInput(t *testing.T) {
	manager, err := NewDefaultManager(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewDefaultManager() error = %v", err)
	}
	tests := []struct {
		name   string
		mutate func(*privatestoragev1.ResolvedNodeVolume)
	}{
		{
			name: "bad claim id",
			mutate: func(v *privatestoragev1.ResolvedNodeVolume) {
				v.ClaimID = "bad/name"
			},
		},
		{
			name: "mismatched backend handle",
			mutate: func(v *privatestoragev1.ResolvedNodeVolume) {
				v.BackendHandle = "claim-2"
			},
		},
		{
			name: "collapsing backend handle",
			mutate: func(v *privatestoragev1.ResolvedNodeVolume) {
				v.ClaimID = "."
				v.BackendHandle = "."
			},
		},
		{
			name: "bad target",
			mutate: func(v *privatestoragev1.ResolvedNodeVolume) {
				v.Target = "/data/../x"
			},
		},
		{
			name: "bad option",
			mutate: func(v *privatestoragev1.ResolvedNodeVolume) {
				v.Options = []string{"shared"}
			},
		},
		{
			name: "runtime incompatible",
			mutate: func(v *privatestoragev1.ResolvedNodeVolume) {
				v.RuntimeCompatibility.SupportsRunsc = false
			},
		},
		{
			name: "unsupported access mode",
			mutate: func(v *privatestoragev1.ResolvedNodeVolume) {
				v.AccessMode = storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_MANY
			},
		},
		{
			name: "unsupported consistency",
			mutate: func(v *privatestoragev1.ResolvedNodeVolume) {
				v.ConsistencyProfile = storagev1.VolumeConsistencyProfile_VOLUME_CONSISTENCY_PROFILE_OBJECT_STORE
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			volume := localVolume()
			tc.mutate(volume)
			if _, err := manager.Publish(context.Background(), "alloc-1", "runsc", volume); err == nil {
				t.Fatal("expected publish error")
			}
		})
	}
}

func TestManagerRejectsInvalidProviderPublishResult(t *testing.T) {
	provider := &recordingProvider{
		published: &runtimevolumev1.PublishedVolume{
			BindingID: "binding-1",
			Backend:   storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
			HostPath:  "relative/path",
			Target:    "/var/lib/app",
			Readonly:  true,
			Options:   []string{"rbind", "ro"},
		},
	}
	manager, err := NewManager(nil, provider)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if _, err := manager.Publish(context.Background(), "alloc-1", "runsc", localVolume()); err == nil || !strings.Contains(err.Error(), "absolute host path") {
		t.Fatalf("Publish() error = %v, want invalid host path", err)
	}
	if provider.unpublished != 1 {
		t.Fatalf("provider unpublished = %d, want rollback unpublish", provider.unpublished)
	}
	if got := manager.List("alloc-1"); len(got) != 0 {
		t.Fatalf("published state = %#v, want none", got)
	}
}

func TestManagerRejectsProviderPublishIdentityDrift(t *testing.T) {
	provider := &recordingProvider{
		published: &runtimevolumev1.PublishedVolume{
			ClaimID:   "claim-2",
			BindingID: "binding-1",
			Backend:   storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
			HostPath:  "/tmp/volume",
			Target:    "/var/lib/app",
			Readonly:  true,
			Options:   []string{"rbind", "ro"},
		},
	}
	manager, err := NewManager(nil, provider)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if _, err := manager.Publish(context.Background(), "alloc-1", "runsc", localVolume()); err == nil || !strings.Contains(err.Error(), "claim id") {
		t.Fatalf("Publish() error = %v, want claim identity drift", err)
	}
	if provider.unpublished != 1 {
		t.Fatalf("provider unpublished = %d, want rollback unpublish", provider.unpublished)
	}
	if got := manager.List("alloc-1"); len(got) != 0 {
		t.Fatalf("published state = %#v, want none", got)
	}
}

func TestManagerUnpublishAndReconcile(t *testing.T) {
	manager, err := NewDefaultManager(t.TempDir(), t.TempDir())
	if err != nil {
		t.Fatalf("NewDefaultManager() error = %v", err)
	}
	if _, err := manager.Publish(context.Background(), "alloc-1", "runsc", localVolume()); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	other := localVolume()
	other.BindingID = "binding-2"
	other.Parameters[LocalParameterVolumeName] = "cache"
	if _, err := manager.Publish(context.Background(), "alloc-2", "runsc", other); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	result, err := manager.Reconcile(context.Background(), []string{"alloc-1"})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.ActiveAllocationCount != 1 || result.RetainedCount != 1 || result.UnpublishedCount != 1 || result.StaleAllocationCount != 1 || result.InvalidVolumeCount != 0 {
		t.Fatalf("Reconcile() = %+v, want active=1 retained=1 unpublished=1 stale=1 invalid=0", result)
	}
	if got := manager.List("alloc-2"); len(got) != 0 {
		t.Fatalf("alloc-2 volumes = %#v, want none", got)
	}
	removed, err := manager.Unpublish(context.Background(), "alloc-1", "binding-1")
	if err != nil {
		t.Fatalf("Unpublish() error = %v", err)
	}
	if len(removed) != 1 || removed[0].GetBindingID() != "binding-1" {
		t.Fatalf("Unpublish() removed = %#v, want binding-1", removed)
	}
	removed, err = manager.Unpublish(context.Background(), "alloc-1", "binding-1")
	if err != nil {
		t.Fatalf("Unpublish() second call error = %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("Unpublish() second removed = %#v, want none", removed)
	}
	if got := manager.List("alloc-1"); len(got) != 0 {
		t.Fatalf("alloc-1 volumes = %#v, want none", got)
	}
}

func TestManagerReconcileRemovesMissingActiveLocalVolume(t *testing.T) {
	root := t.TempDir()
	localRoot := filepath.Join(root, "local")
	manager, err := NewDefaultManager(root, localRoot)
	if err != nil {
		t.Fatalf("NewDefaultManager() error = %v", err)
	}
	published, err := manager.Publish(context.Background(), "alloc-1", "runsc", localVolume())
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if err := os.RemoveAll(published.GetHostPath()); err != nil {
		t.Fatalf("RemoveAll() error = %v", err)
	}

	result, err := manager.Reconcile(context.Background(), []string{"alloc-1"})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.ActiveAllocationCount != 1 || result.RetainedCount != 0 || result.UnpublishedCount != 1 || result.StaleAllocationCount != 0 || result.InvalidVolumeCount != 1 {
		t.Fatalf("Reconcile() = %+v, want active=1 retained=0 unpublished=1 stale=0 invalid=1", result)
	}
	if got := manager.List("alloc-1"); len(got) != 0 {
		t.Fatalf("alloc-1 volumes = %#v, want none", got)
	}
}

func TestManagerReconcileKeepsStateOnTransientValidationError(t *testing.T) {
	validationErr := errors.New("stat backend temporarily unavailable")
	provider := &recordingProvider{validationErr: validationErr}
	manager, err := NewManager(nil, provider)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if _, err := manager.Publish(context.Background(), "alloc-1", "runsc", localVolume()); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	result, err := manager.Reconcile(context.Background(), []string{"alloc-1"})
	if !errors.Is(err, validationErr) {
		t.Fatalf("Reconcile() error = %v, want %v", err, validationErr)
	}
	if result.ActiveAllocationCount != 1 || result.RetainedCount != 0 || result.UnpublishedCount != 0 || result.StaleAllocationCount != 0 || result.InvalidVolumeCount != 0 {
		t.Fatalf("Reconcile() = %+v, want active=1 retained=0 unpublished=0 stale=0 invalid=0", result)
	}
	if got := manager.List("alloc-1"); len(got) != 1 {
		t.Fatalf("alloc-1 volumes = %#v, want retained state", got)
	}
	if provider.unpublished != 0 {
		t.Fatalf("provider unpublished = %d, want 0", provider.unpublished)
	}
	health := manager.Health()
	if health.GetStatus() != runtimevolumev1.VolumeManagerStatus_VOLUME_MANAGER_STATUS_ERROR || !strings.Contains(health.GetLastReconcileError(), validationErr.Error()) {
		t.Fatalf("health after reconcile error = %#v, want error with validation failure", health)
	}
}

func TestManagerReconcileKeepsStateWhenProviderMissing(t *testing.T) {
	manager, err := NewManager(nil, &recordingProvider{})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if _, err := manager.Publish(context.Background(), "alloc-1", "runsc", localVolume()); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	delete(manager.providers, storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL)

	result, err := manager.Reconcile(context.Background(), []string{"alloc-1"})
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("Reconcile() error = %v, want unsupported backend", err)
	}
	if result.ActiveAllocationCount != 1 || result.RetainedCount != 0 || result.UnpublishedCount != 0 || result.StaleAllocationCount != 0 || result.InvalidVolumeCount != 0 {
		t.Fatalf("Reconcile() = %+v, want active=1 retained=0 unpublished=0 stale=0 invalid=0", result)
	}
	if got := manager.List("alloc-1"); len(got) != 1 {
		t.Fatalf("alloc-1 volumes = %#v, want retained state", got)
	}
}

func TestManagerReconcileKeepsInactiveStateWhenProviderMissing(t *testing.T) {
	manager, err := NewManager(nil, &recordingProvider{})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if _, err := manager.Publish(context.Background(), "alloc-1", "runsc", localVolume()); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	delete(manager.providers, storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL)

	result, err := manager.Reconcile(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("Reconcile() error = %v, want unsupported backend", err)
	}
	if result.ActiveAllocationCount != 0 || result.RetainedCount != 0 || result.UnpublishedCount != 0 || result.StaleAllocationCount != 1 || result.InvalidVolumeCount != 0 {
		t.Fatalf("Reconcile() = %+v, want active=0 retained=0 unpublished=0 stale=1 invalid=0", result)
	}
	if got := manager.List("alloc-1"); len(got) != 1 {
		t.Fatalf("alloc-1 volumes = %#v, want retained state", got)
	}
}

func TestManagerReconcileCountsOnlySuccessfulUnpublish(t *testing.T) {
	unpublishErr := errors.New("backend unpublish failed")
	provider := &recordingProvider{unpublishErr: unpublishErr}
	manager, err := NewManager(nil, provider)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if _, err := manager.Publish(context.Background(), "alloc-1", "runsc", localVolume()); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	result, err := manager.Reconcile(context.Background(), nil)
	if !errors.Is(err, unpublishErr) {
		t.Fatalf("Reconcile() error = %v, want %v", err, unpublishErr)
	}
	if result.ActiveAllocationCount != 0 || result.RetainedCount != 0 || result.UnpublishedCount != 0 || result.StaleAllocationCount != 1 || result.InvalidVolumeCount != 0 {
		t.Fatalf("Reconcile() = %+v, want active=0 retained=0 unpublished=0 stale=1 invalid=0", result)
	}
	if got := manager.List("alloc-1"); len(got) != 1 {
		t.Fatalf("alloc-1 volumes = %#v, want retained state after failed unpublish", got)
	}
	health := manager.Health()
	if health.GetStatus() != runtimevolumev1.VolumeManagerStatus_VOLUME_MANAGER_STATUS_ERROR ||
		health.GetLastReconcileUnpublishedCount() != 0 ||
		health.GetLastReconcileStaleAllocationCount() != 1 {
		t.Fatalf("health after failed unpublish = %#v", health)
	}
}

func TestManagerReconcilePreservesConcurrentRepublish(t *testing.T) {
	provider := &recordingProvider{published: &runtimevolumev1.PublishedVolume{
		ClaimID:   "claim-1",
		BindingID: "binding-1",
		Backend:   storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
		HostPath:  "/tmp/volume-old",
		Target:    "/var/lib/app",
		Readonly:  true,
		Options:   []string{"rbind", "ro"},
	}}
	manager, err := NewManager(nil, provider)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if _, err := manager.Publish(context.Background(), "alloc-1", "runsc", localVolume()); err != nil {
		t.Fatalf("Publish(old) error = %v", err)
	}
	provider.unpublishHook = func(ctx context.Context) {
		provider.published = &runtimevolumev1.PublishedVolume{
			ClaimID:   "claim-1",
			BindingID: "binding-1",
			Backend:   storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
			HostPath:  "/tmp/volume-new",
			Target:    "/var/lib/app",
			Readonly:  true,
			Options:   []string{"rbind", "ro"},
		}
		if _, err := manager.Publish(ctx, "alloc-1", "runsc", localVolume()); err != nil {
			t.Errorf("Publish(new) during reconcile error = %v", err)
		}
	}

	result, err := manager.Reconcile(context.Background(), nil)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if result.StaleAllocationCount != 1 || result.UnpublishedCount != 0 {
		t.Fatalf("Reconcile() = %+v, want stale=1 unpublished=0 for concurrently replaced state", result)
	}
	got, ok := manager.Get("alloc-1", "binding-1")
	if !ok {
		t.Fatal("Get(new) not found")
	}
	if got.GetHostPath() != "/tmp/volume-new" {
		t.Fatalf("HostPath after concurrent republish = %q, want /tmp/volume-new", got.GetHostPath())
	}
}

func TestManagerRejectsDuplicateProviders(t *testing.T) {
	_, err := NewManager(nil, &recordingProvider{}, &recordingProvider{})
	if err == nil {
		t.Fatal("expected duplicate provider error")
	}
}

func TestManagerRejectsInvalidProviderCapabilities(t *testing.T) {
	_, err := NewManager(nil, &recordingProvider{capabilities: ProviderCapabilities{
		Backend: storagev1.VolumeBackend_VOLUME_BACKEND_NFS,
	}})
	if err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("NewManager() error = %v, want provider capability mismatch", err)
	}
}

func TestManagerRollsBackProviderPublishWhenStateSaveFails(t *testing.T) {
	storeErr := errors.New("save failed")
	provider := &recordingProvider{}
	manager, err := NewManager(failingSaveStore{err: storeErr}, provider)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	if _, err := manager.Publish(context.Background(), "alloc-1", "runsc", localVolume()); !errors.Is(err, storeErr) {
		t.Fatalf("Publish() error = %v, want %v", err, storeErr)
	}
	if provider.unpublished != 1 {
		t.Fatalf("provider unpublished = %d, want rollback unpublish", provider.unpublished)
	}
	if got := manager.List("alloc-1"); len(got) != 0 {
		t.Fatalf("published state = %#v, want rolled back", got)
	}
}

type failingSaveStore struct {
	err error
}

func (s failingSaveStore) Load(context.Context) (map[string][]*runtimevolumev1.PublishedVolume, error) {
	return map[string][]*runtimevolumev1.PublishedVolume{}, nil
}

func (s failingSaveStore) Save(context.Context, map[string][]*runtimevolumev1.PublishedVolume) error {
	return s.err
}

type recordingProvider struct {
	unpublished   int
	validationErr error
	unpublishErr  error
	unpublishHook func(context.Context)
	published     *runtimevolumev1.PublishedVolume
	capabilities  ProviderCapabilities
}

func (p *recordingProvider) Backend() storagev1.VolumeBackend {
	return storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL
}

func (p *recordingProvider) Capabilities() ProviderCapabilities {
	if p.capabilities.Backend != storagev1.VolumeBackend_VOLUME_BACKEND_UNSPECIFIED {
		return p.capabilities
	}
	return ProviderCapabilities{
		Backend: p.Backend(),
		AccessModes: []storagev1.VolumeAccessMode{
			storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE,
		},
		ConsistencyProfiles: []storagev1.VolumeConsistencyProfile{
			storagev1.VolumeConsistencyProfile_VOLUME_CONSISTENCY_PROFILE_POSIX,
		},
		RuntimeCompatibility: &storagev1.VolumeRuntimeCompatibility{
			SupportsRunc:  true,
			SupportsRunsc: true,
		},
	}
}

func (p *recordingProvider) Publish(context.Context, string, *privatestoragev1.ResolvedNodeVolume) (*runtimevolumev1.PublishedVolume, error) {
	if p.published != nil {
		return clonePublished(p.published), nil
	}
	return &runtimevolumev1.PublishedVolume{
		ClaimID:   "claim-1",
		BindingID: "binding-1",
		Backend:   p.Backend(),
		HostPath:  "/tmp/volume",
		Target:    "/var/lib/app",
		Readonly:  true,
		Options:   []string{"rbind", "ro"},
	}, nil
}

func (p *recordingProvider) Unpublish(ctx context.Context, _ string, _ *runtimevolumev1.PublishedVolume) error {
	p.unpublished++
	if p.unpublishHook != nil {
		p.unpublishHook(ctx)
	}
	return p.unpublishErr
}

func (p *recordingProvider) Delete(context.Context, string, string) error {
	return nil
}

func (p *recordingProvider) ValidatePublished(context.Context, string, *runtimevolumev1.PublishedVolume) error {
	return p.validationErr
}

func localVolume() *privatestoragev1.ResolvedNodeVolume {
	return &privatestoragev1.ResolvedNodeVolume{
		ClaimID:            "claim-1",
		BackendHandle:      "claim-1",
		BindingID:          "binding-1",
		Backend:            storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
		AccessMode:         storagev1.VolumeAccessMode_VOLUME_ACCESS_MODE_READ_WRITE_ONCE,
		ConsistencyProfile: storagev1.VolumeConsistencyProfile_VOLUME_CONSISTENCY_PROFILE_POSIX,
		Target:             "/var/lib/app",
		Readonly:           true,
		Options:            []string{"nodev", "rbind", "rw"},
		Parameters: map[string]string{
			LocalParameterNamespace:  "default",
			LocalParameterServiceID:  "svc-123",
			LocalParameterVolumeName: "data",
		},
		RuntimeCompatibility: &storagev1.VolumeRuntimeCompatibility{
			SupportsRunc:  true,
			SupportsRunsc: true,
		},
	}
}
