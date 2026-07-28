package volumes

import (
	"context"
	"errors"
	"reflect"
	"testing"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	privatestoragev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/storage/v1"
)

func TestPublishForStartReturnsRuntimeMountsWithoutMutatingRequest(t *testing.T) {
	publisher := &fakePublisher{
		published: []*privatestoragev1.PublishedNodeVolume{{
			HostPath: "/var/lib/axern/volumes/default/svc-123/data",
			Target:   "/var/lib/app",
			Options:  []string{"rbind", "nodev", "rw"},
		}},
	}
	coordinator := NewCoordinator(Options{Publisher: publisher})
	req := &runtime.StartRequest{
		RuntimeTemplate: &runtime.RuntimeTemplate{Sandbox: "runsc"},
		ContainerID:     "alloc-1",
		NodeVolumes: []*privatestoragev1.ResolvedNodeVolume{
			newTestLocalNodeVolume("default", "svc-123", "data", "/var/lib/app", false, []string{"rbind", "nodev", "rw"}),
		},
	}

	result, err := coordinator.PublishForStart(context.Background(), req)
	if err != nil {
		t.Fatalf("PublishForStart() error = %v", err)
	}
	if len(req.GetMounts()) != 0 {
		t.Fatalf("request mounts mutated: %#v", req.GetMounts())
	}
	if len(result.Published) != 1 {
		t.Fatalf("published volumes = %d, want 1", len(result.Published))
	}
	mount := result.RuntimeMounts[0]
	if mount.GetType() != "bind" || mount.GetSource() != "/var/lib/axern/volumes/default/svc-123/data" || mount.GetTarget() != "/var/lib/app" {
		t.Fatalf("runtime mount = %#v, want managed bind mount", mount)
	}
	if !reflect.DeepEqual(mount.GetOptions(), []string{"rbind", "nodev", "rw"}) {
		t.Fatalf("runtime mount options = %#v", mount.GetOptions())
	}
}

func TestPublishForStartRequiresVolumePublisher(t *testing.T) {
	coordinator := NewCoordinator(Options{})
	req := &runtime.StartRequest{
		RuntimeTemplate: &runtime.RuntimeTemplate{Sandbox: "runsc"},
		ContainerID:     "alloc-1",
		NodeVolumes: []*privatestoragev1.ResolvedNodeVolume{
			newTestLocalNodeVolume("default", "svc-123", "data", "/data", false, nil),
		},
	}

	if _, err := coordinator.PublishForStart(context.Background(), req); err == nil {
		t.Fatal("expected missing volume manager client error")
	}
}

func TestPublishForStartPropagatesVolumePublisherError(t *testing.T) {
	coordinator := NewCoordinator(Options{
		Publisher: &fakePublisher{err: errTestVolumePublish},
	})
	req := &runtime.StartRequest{
		RuntimeTemplate: &runtime.RuntimeTemplate{Sandbox: "runsc"},
		ContainerID:     "alloc-1",
		NodeVolumes: []*privatestoragev1.ResolvedNodeVolume{
			newTestLocalNodeVolume("default", "svc-123", "data", "/data", false, nil),
		},
	}

	if _, err := coordinator.PublishForStart(context.Background(), req); !errors.Is(err, errTestVolumePublish) {
		t.Fatalf("PublishForStart() error = %v, want test volume error", err)
	}
}

func TestNormalizeAllocationIDs(t *testing.T) {
	got := NormalizeAllocationIDs([]string{" alloc-b ", "", "alloc-a", "alloc-b"})
	want := []string{"alloc-a", "alloc-b"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("NormalizeAllocationIDs() = %#v, want %#v", got, want)
	}
}

func TestReconcileNormalizesActiveAllocationIDs(t *testing.T) {
	publisher := &fakePublisher{}
	coordinator := NewCoordinator(Options{
		Publisher: publisher,
		ActiveAllocationIDs: func() []string {
			return []string{" alloc-b ", "", "alloc-a", "alloc-b"}
		},
	})

	if err := coordinator.Reconcile(context.Background()); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	want := []string{"alloc-a", "alloc-b"}
	if !reflect.DeepEqual(publisher.reconcileActive, want) {
		t.Fatalf("reconcile active ids = %#v, want %#v", publisher.reconcileActive, want)
	}
}

var errTestVolumePublish = errors.New("test volume publish error")

type fakePublisher struct {
	published       []*privatestoragev1.PublishedNodeVolume
	listed          []*privatestoragev1.PublishedNodeVolume
	reconcileActive []string
	err             error
}

func (f fakePublisher) PublishAll(context.Context, string, string, []*privatestoragev1.ResolvedNodeVolume) ([]*privatestoragev1.PublishedNodeVolume, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.published, nil
}

func (f fakePublisher) ListPublishedVolumes(context.Context, string) ([]*privatestoragev1.PublishedNodeVolume, error) {
	return f.listed, nil
}

func (f fakePublisher) UnpublishAllocation(context.Context, string) ([]*privatestoragev1.VolumeReleaseObservation, error) {
	return nil, nil
}

func (f fakePublisher) DeleteVolume(context.Context, string, storagev1.VolumeBackend, string) error {
	return nil
}

func (f *fakePublisher) ReconcileActiveAllocations(_ context.Context, active []string) (*runtimevolumev1.ReconcileVolumesResponse, error) {
	f.reconcileActive = append([]string(nil), active...)
	return &runtimevolumev1.ReconcileVolumesResponse{}, nil
}

func newTestLocalNodeVolume(namespace, serviceID, name, target string, readonly bool, options []string) *privatestoragev1.ResolvedNodeVolume {
	return &privatestoragev1.ResolvedNodeVolume{
		ClaimID:  namespace + "/" + serviceID + "/" + name,
		VolumeID: name,
		Backend:  storagev1.VolumeBackend_VOLUME_BACKEND_LOCAL,
		Target:   target,
		Readonly: readonly,
		Options:  append([]string(nil), options...),
		Parameters: map[string]string{
			"namespace":   namespace,
			"service_id":  serviceID,
			"volume_name": name,
		},
		RuntimeCompatibility: &storagev1.VolumeRuntimeCompatibility{
			SupportsRunc:  true,
			SupportsRunsc: true,
		},
	}
}
