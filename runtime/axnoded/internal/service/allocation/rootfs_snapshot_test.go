package allocation

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/rootfssnapshot"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	environmentv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/environment/v1"
	"github.com/stretchr/testify/require"
)

func TestRootfsSnapshotSealingErrorPreservesCause(t *testing.T) {
	want := errors.New("publisher unavailable")
	err := WrapRootfsSnapshotSealingError(want)
	require.True(t, IsRootfsSnapshotSealingError(err))
	require.ErrorIs(t, err, want)
	require.False(t, IsRootfsSnapshotSealingError(want))
}

type snapshotPublisher struct{ calls int }

func (p *snapshotPublisher) Publish(_ context.Context, request rootfssnapshot.Request) (*apipb.RootfsSnapshotSealingResult, error) {
	p.calls++
	if _, err := io.Copy(io.Discard, request.UpperLayer); err != nil {
		return nil, err
	}
	return &apipb.RootfsSnapshotSealingResult{
		ImageRef:        "registry.local/snapshots@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ImageDescriptor: &environmentv1.OciImageDescriptor{Digest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		PlatformOS:      "linux", PlatformArch: "amd64",
	}, nil
}

func TestRootfsSnapshotReceiptMakesDeleteRetryIdempotent(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	fixture := newTestAllocationController(t, handler)
	fixture.controller.config.ControlPlaneNodeID = "node-a"
	publisher := &snapshotPublisher{}
	fixture.controller.rootfsSnapshots = publisher
	const allocationID = "allocation-snapshot"
	const baseRef = "registry.local/base@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	require.NoError(t, fixture.controller.StoreAllocationIntent(allocationID, "node-a",
		"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", time.Now().Add(time.Minute),
		&commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{EphemeralStorageBytes: 1 << 20}}, nil, nil, &commonv1.RootfsSnapshot{}))
	fixture.controller.stateMu.Lock()
	record := fixture.controller.allocationStates[allocationID].record
	record.Environment = &apipb.ResolvedEnvironment{Rootfs: &apipb.RootfsConfig{Type: apipb.RootfsSrcType_IMAGE, Source: &apipb.RootfsConfig_ImageUrl{ImageUrl: baseRef}}}
	fixture.controller.stateMu.Unlock()
	require.NoError(t, fixture.controller.persistAllocationRecord(record))
	storeTestContainer(t, fixture, allocationID, "runsc")

	request := &apipb.RootfsSnapshotSealingRequest{BaseImageRef: baseRef}
	first, err := fixture.controller.sealRootfsSnapshot(context.Background(), allocationID, request)
	require.NoError(t, err)
	second, err := fixture.controller.sealRootfsSnapshot(context.Background(), allocationID, request)
	require.NoError(t, err)
	require.Equal(t, first.GetImageRef(), second.GetImageRef())
	require.Equal(t, 1, publisher.calls)
	require.Equal(t, 1, handler.snapshotCalls)

	var receipt apipb.RootfsSnapshotReceipt
	require.NoError(t, fixture.controller.store.GetRecord(config.RootfsSnapshotReceiptBucket, allocationID, &receipt))
	require.NoError(t, fixture.controller.AcknowledgeRootfsSnapshotRelease(allocationID, "node-a"))
	require.Error(t, fixture.controller.store.GetRecord(config.RootfsSnapshotReceiptBucket, allocationID, &receipt))
}

func TestRootfsSnapshotAcceptsEquivalentRepositoryAliases(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	fixture := newTestAllocationController(t, handler)
	fixture.controller.config.ControlPlaneNodeID = "node-a"
	fixture.controller.rootfsSnapshots = &snapshotPublisher{}
	const allocationID = "allocation-snapshot"
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	require.NoError(t, fixture.controller.StoreAllocationIntent(allocationID, "node-a",
		"sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc", time.Now().Add(time.Minute),
		&commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{EphemeralStorageBytes: 1 << 20}}, nil, nil, &commonv1.RootfsSnapshot{}))
	fixture.controller.stateMu.Lock()
	record := fixture.controller.allocationStates[allocationID].record
	record.Environment = &apipb.ResolvedEnvironment{Rootfs: &apipb.RootfsConfig{
		Type:   apipb.RootfsSrcType_IMAGE,
		Source: &apipb.RootfsConfig_ImageUrl{ImageUrl: "index.docker.io/library/python@" + digest},
	}}
	fixture.controller.stateMu.Unlock()
	require.NoError(t, fixture.controller.persistAllocationRecord(record))
	storeTestContainer(t, fixture, allocationID, "runsc")
	result, err := fixture.controller.sealRootfsSnapshot(context.Background(), allocationID, &apipb.RootfsSnapshotSealingRequest{
		BaseImageRef: "python@" + digest,
	})
	if err != nil {
		t.Fatalf("sealRootfsSnapshot() error = %v", err)
	}
	if result == nil {
		t.Fatal("sealRootfsSnapshot() result is nil")
	}
}

func TestRootfsSnapshotRejectsContractMismatch(t *testing.T) {
	fixture := newTestAllocationController(t, &runtimeSpyHandler{name: "runsc"})
	require.NoError(t, fixture.controller.StoreAllocationIntent("allocation-snapshot", "node-a",
		"sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd", time.Now().Add(time.Minute),
		&commonv1.ResourceSpec{Limits: &commonv1.ResourceQuantity{EphemeralStorageBytes: 1 << 20}}, nil, nil, &commonv1.RootfsSnapshot{}))
	fixture.controller.stateMu.Lock()
	fixture.controller.allocationStates["allocation-snapshot"].record.Environment = &apipb.ResolvedEnvironment{Rootfs: &apipb.RootfsConfig{Type: apipb.RootfsSrcType_IMAGE, Source: &apipb.RootfsConfig_ImageUrl{ImageUrl: "registry.local/base@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}}}
	fixture.controller.stateMu.Unlock()
	_, err := fixture.controller.sealRootfsSnapshot(context.Background(), "allocation-snapshot", &apipb.RootfsSnapshotSealingRequest{BaseImageRef: "registry.local/base@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"})
	require.Error(t, err)
}
