package api

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/imagemgr/imagefsd"
	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"github.com/cofy-x/axern/runtime/imagemgr/ossloop"
)

// mustNewHttpWorker creates an HttpWorker for testing, failing the test on error.
func mustNewHttpWorker(t *testing.T, mgr imagefsd.Manager) *HttpWorker {
	t.Helper()
	w, err := NewHttpWorker(&HttpWorkerConfig{
		Manager:        mgr,
		OSSLoopManager: newMockOSSLoopManager(),
	})
	if err != nil {
		t.Fatalf("NewHttpWorker: %v", err)
	}
	w.mountStore = openTestMountStore(t)
	return w
}

func openTestMountStore(t *testing.T) *mountstore.Store {
	t.Helper()
	store, err := mountstore.Open(filepath.Join(t.TempDir(), "mounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

type mockOSSLoopManager struct {
	mounts map[string]string
}

func newMockOSSLoopManager() *mockOSSLoopManager {
	return &mockOSSLoopManager{
		mounts: make(map[string]string),
	}
}

func (m *mockOSSLoopManager) EnsureMounted(id, imagePath string) (string, error) {
	path := "/rootfs/" + id
	m.mounts[id] = path
	return path, nil
}

func (m *mockOSSLoopManager) EffectiveLowerDirs(id string) ([]string, error) {
	if _, ok := m.mounts[id]; !ok {
		return nil, fmt.Errorf("oss rootfs %s is not mounted", id)
	}
	return []string{"/rootfs-lower/" + id, "/rootfs-support"}, nil
}

func (m *mockOSSLoopManager) ReleaseResource(id string) (ossloop.UnmountResult, error) {
	path, ok := m.mounts[id]
	if !ok {
		return ossloop.UnmountResult{Released: true}, nil
	}
	delete(m.mounts, id)
	return ossloop.UnmountResult{MountPath: path, Released: true}, nil
}

type mockDaemon struct {
	meta        imagefsd.DaemonMeta
	mountFunc   func() error
	unmountFunc func() error
}

func (m *mockDaemon) Mount() error {
	if m.mountFunc != nil {
		return m.mountFunc()
	}
	return nil
}

func (m *mockDaemon) Unmount() error {
	if m.unmountFunc != nil {
		return m.unmountFunc()
	}
	return nil
}

func (m *mockDaemon) MountPoint() string {
	return m.meta.MountPoint
}

func (m *mockDaemon) Name() string {
	return m.meta.Name
}

func (m *mockDaemon) IsAlive() bool {
	return true
}

type mockManager struct {
	createDaemonFunc  func(opts *imagefsd.DaemonCreateOpt) error
	getDaemonFunc     func(id string) *imagefsd.Daemon
	cleanupDaemonFunc func(daemonID string) error
	listDaemonsFunc   func() []imagefsd.DaemonInfo
	chunkDBStatsFunc  func() (*imagefsd.ChunkDBStats, error)
	localityStatsFunc func() (*imagefsd.LocalityStats, error)
	daemons           map[string]*mockDaemon
}

func newMockManager() *mockManager {
	return &mockManager{
		daemons: make(map[string]*mockDaemon),
	}
}

func (m *mockManager) CreateDaemon(opts *imagefsd.DaemonCreateOpt) error {
	if m.createDaemonFunc != nil {
		return m.createDaemonFunc(opts)
	}

	daemon := &mockDaemon{
		meta: imagefsd.DaemonMeta{
			ID:         opts.ID,
			Name:       opts.Name,
			MountPoint: "/mnt/" + opts.ID,
		},
	}
	m.daemons[opts.ID] = daemon
	return nil
}

func (m *mockManager) GetDaemon(id string) *imagefsd.Daemon {
	if m.getDaemonFunc != nil {
		return m.getDaemonFunc(id)
	}
	return nil
}

func (m *mockManager) CleanupDaemon(daemonID string) error {
	if m.cleanupDaemonFunc != nil {
		return m.cleanupDaemonFunc(daemonID)
	}
	if daemonID == "" {
		return fmt.Errorf("daemon ID is empty")
	}
	delete(m.daemons, daemonID)
	return nil
}

func (m *mockManager) ListDaemons() []imagefsd.DaemonInfo {
	if m.listDaemonsFunc != nil {
		return m.listDaemonsFunc()
	}
	return []imagefsd.DaemonInfo{}
}

func (m *mockManager) ChunkDBStats() (*imagefsd.ChunkDBStats, error) {
	if m.chunkDBStatsFunc != nil {
		return m.chunkDBStatsFunc()
	}
	return nil, fmt.Errorf("chunkdb stats unavailable")
}

func (m *mockManager) LocalityStats() (*imagefsd.LocalityStats, error) {
	if m.localityStatsFunc != nil {
		return m.localityStatsFunc()
	}
	return nil, fmt.Errorf("locality stats unavailable")
}
