package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type recordingPersistentViewProvider struct {
	reconcileCalls int
	retained       map[string]struct{}
	removed        []string
	removeErr      error
	reconcileErr   error
}

func (p *recordingPersistentViewProvider) Prepare(context.Context, string, rootfsview.Request) (rootfsview.View, error) {
	return rootfsview.View{}, nil
}

func (p *recordingPersistentViewProvider) Remove(_ context.Context, containerID string) error {
	p.removed = append(p.removed, containerID)
	return p.removeErr
}

func (p *recordingPersistentViewProvider) ReconcilePersistentViews(_ context.Context, _ string, retained map[string]struct{}) error {
	p.reconcileCalls++
	p.retained = make(map[string]struct{}, len(retained))
	for id := range retained {
		p.retained[id] = struct{}{}
	}
	return p.reconcileErr
}

func TestReconcilePersistentStorageUsesOnlyRuntimeInventoryForLiveness(t *testing.T) {
	views := &recordingPersistentViewProvider{}
	capacity := newTestWritableCapacityManager(t, 0)
	require.NoError(t, capacity.Reserve("orphan", "runsc", 1, 1))

	err := reconcilePersistentStorage(
		context.Background(),
		"runsc",
		t.TempDir(),
		map[string]struct{}{},
		views,
		capacity,
	)
	require.NoError(t, err)
	assert.Equal(t, 1, views.reconcileCalls)
	assert.Empty(t, views.retained)
	assert.Equal(t, []string{"orphan"}, views.removed)
	assert.NotContains(t, capacity.reservations, "orphan")
}

func TestReconcilePersistentStorageRetainsRuntimeInventory(t *testing.T) {
	views := &recordingPersistentViewProvider{}

	err := reconcilePersistentStorage(
		context.Background(),
		"runsc",
		t.TempDir(),
		map[string]struct{}{"live": {}},
		views,
		nil,
	)
	require.NoError(t, err)
	assert.Equal(t, map[string]struct{}{"live": {}}, views.retained)
}

func TestReconcilePersistentStoragePreservesReservationWhenCleanupFails(t *testing.T) {
	views := &recordingPersistentViewProvider{removeErr: errors.New("projection is still mounted")}
	capacity := newTestWritableCapacityManager(t, 0)
	require.NoError(t, capacity.Reserve("orphan", "runsc", 1, 1))

	err := reconcilePersistentStorage(
		context.Background(),
		"runsc",
		t.TempDir(),
		map[string]struct{}{},
		views,
		capacity,
	)
	require.ErrorContains(t, err, "cleanup stale ephemeral storage reservation")
	assert.Equal(t, []string{"orphan"}, views.removed)
	assert.Contains(t, capacity.reservations, "orphan", "failed cleanup must retain the durable reservation")
}

func TestReconcilePersistentStorageReportsActiveViewDegradation(t *testing.T) {
	views := &recordingPersistentViewProvider{reconcileErr: errors.New("active backing identity changed")}

	err := reconcilePersistentStorage(
		context.Background(),
		"runsc",
		t.TempDir(),
		map[string]struct{}{"live": {}},
		views,
		nil,
	)
	require.ErrorContains(t, err, "active backing identity changed")
	assert.Empty(t, views.removed)
}
