package environmentcache

import (
	"errors"
	"sync"
	"testing"
	"time"

	api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/stretchr/testify/require"
)

type releaseMounter struct {
	mockMounter
	err     error
	desired []string
	calls   int
}

func (m *releaseMounter) Reconcile(ids []string) error {
	m.calls++
	m.desired = append([]string(nil), ids...)
	return m.err
}

func TestManagedRootfsReleaseKeepsFailedDeliveryForRetry(t *testing.T) {
	m := &releaseMounter{}
	cache := NewEnvironmentCache(m)
	cfg := RootfsConfig{SrcType: api.RootfsSrcType_IMAGE, LeaseID: "release"}
	root, err := cache.GetRootfs(cfg)
	require.NoError(t, err)
	require.NoError(t, root.IncActiveRef())
	active, err := cache.GetRootfs(RootfsConfig{SrcType: api.RootfsSrcType_IMAGE, LeaseID: "active"})
	require.NoError(t, err)
	require.NoError(t, active.IncActiveRef())
	want := errors.New("imagemgr unavailable")
	m.err = want
	released, err := root.ReleaseActiveRef()
	require.True(t, released)
	require.ErrorIs(t, err, want)
	require.Equal(t, []string{"active"}, m.desired)
	require.True(t, cache.hasReleasedRootfs())
	require.Error(t, root.IncActiveRef())
	require.Equal(t, 0, m.UmountCount(), "managed images have no parallel direct unmount")
	released, err = root.ReleaseActiveRef()
	require.False(t, released)
	require.NoError(t, err)
	require.Equal(t, 1, m.calls, "duplicate release must not deliver twice")
	require.ErrorIs(t, cache.EvictIdleEnvironment("already-removed", RetentionReasonSelfTest), want)
	m.err = nil
	require.NoError(t, cache.sweep(time.Now()), "an idle sweep must retry undelivered releases")
	require.False(t, cache.hasReleasedRootfs())
	require.Equal(t, []string{"active"}, m.desired)
	require.NotContains(t, cache.rootfsMap, cfg)
	require.NoError(t, active.IncActiveRef(), "retry cannot retire a live rootfs")
}

func TestCleanupSweeperStartsWithRetentionDisabled(t *testing.T) {
	cache := NewEnvironmentCache(&mockMounter{})
	cache.Start()
	defer cache.Close()
	require.NotNil(t, cache.sweeperStop)
	require.NotNil(t, cache.sweeperDone)
}

func TestActiveEnvironmentReleaseDoesNotRetryUnrelatedCleanup(t *testing.T) {
	m := &releaseMounter{err: errors.New("unavailable")}
	cache := NewEnvironmentCache(m)
	cache.rootfsMap[RootfsConfig{LeaseID: "pending"}] = &rootfsEntry{rootfs: &RootFS{released: true}}
	environment, err := addTestEnvironmentCache(cache, newTestFR("active", "/active"))
	require.NoError(t, err)
	environment.IncRef()
	environment.IncRef()
	environment.DecRef()
	require.EqualValues(t, 1, environment.refcnt)
	require.Zero(t, m.calls, "non-final release must not call imagemgr for another root")
}

func TestSweepRetriesOldCleanupWhileExpiringLocalEnvironments(t *testing.T) {
	m := &releaseMounter{}
	cache := NewEnvironmentCache(m)
	cache.ConfigureRetention(time.Minute, 8)
	cfg := RootfsConfig{LeaseID: "pending"}
	cache.rootfsMap[cfg] = &rootfsEntry{rootfs: &RootFS{released: true}}
	environment, err := addTestEnvironmentCache(cache, newTestFR("expires", "/expires"))
	require.NoError(t, err)
	environment.IncRef()
	environment.DecRef()
	require.NoError(t, cache.sweep(environment.ExpireAt()))
	require.Equal(t, 1, m.calls, "a nonempty local eviction batch must not starve image cleanup")
	require.NotContains(t, cache.rootfsMap, cfg)
	require.Nil(t, cache.GetPreparedEnvironment("expires"))
}

func TestRootfsReferenceUnderflowDoesNotMutateOwnership(t *testing.T) {
	root := &RootFS{retainedRefs: 1}
	released, err := root.ReleaseActiveRef()
	require.False(t, released)
	require.Error(t, err)
	require.Error(t, root.MoveActiveToRetained())
	require.EqualValues(t, 0, root.activeRefs)
	require.EqualValues(t, 1, root.retainedRefs)
	require.False(t, root.referencesReleased())
}

func TestConcurrentFinalRootfsReleaseOwnsCleanupOnce(t *testing.T) {
	entered := make(chan struct{})
	finish := make(chan struct{})
	root := &RootFS{activeRefs: 2, releaseMount: func() error {
		close(entered)
		<-finish
		return nil
	}}
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { _, _ = root.ReleaseActiveRef() })
	}
	<-entered
	require.Error(t, root.IncActiveRef())
	released, err := root.ReleaseActiveRef()
	require.NoError(t, err)
	require.False(t, released)
	close(finish)
	wg.Wait()
}

func TestEvictionReportsCleanupFailure(t *testing.T) {
	want := errors.New("unmount busy")
	cache := NewEnvironmentCache(&mockMounter{})
	root := &RootFS{activeRefs: 1, releaseMount: func() error { return want }}
	err := cache.executeEvictions([]retentionEviction{{environment: &PreparedEnvironment{ID: "env"}, rootfs: root}})
	require.ErrorIs(t, err, want)
}
