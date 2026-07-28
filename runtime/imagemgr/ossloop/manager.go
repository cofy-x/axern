package ossloop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cofy-x/axern/runtime/imagemgr/internal/rootfssupport"
)

type Record struct {
	ID        string `json:"id"`
	ImagePath string `json:"image_path"`
	MountPath string `json:"mount_path"`
}

type Config struct {
	Root      string
	MountFunc func(imagePath, lowerPath, targetPath, supportPath string) error
	UnmountFn func(lowerPath, targetPath string) error
	MountedFn func(targetPath string) (bool, error)
}

type UnmountResult struct {
	MountPath string
	Released  bool
}

type Manager struct {
	mu         sync.Mutex
	root       string
	mountsDir  string
	lowersDir  string
	supportDir string
	recordsDir string
	states     map[string]*mountState
	mountFn    func(imagePath, lowerPath, targetPath, supportPath string) error
	unmountFn  func(lowerPath, targetPath string) error
	mountedFn  func(targetPath string) (bool, error)
}

type mountState struct {
	record Record
	refs   int
}

func NewManager(cfg *Config) (*Manager, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.Root == "" {
		return nil, fmt.Errorf("root is required")
	}

	mgr := &Manager{
		root:       cfg.Root,
		mountsDir:  filepath.Join(cfg.Root, "mounts"),
		lowersDir:  filepath.Join(cfg.Root, "lowers"),
		supportDir: filepath.Join(cfg.Root, "support", "fs"),
		recordsDir: filepath.Join(cfg.Root, "records"),
		states:     make(map[string]*mountState),
		mountFn:    cfg.MountFunc,
		unmountFn:  cfg.UnmountFn,
		mountedFn:  cfg.MountedFn,
	}
	if mgr.mountFn == nil {
		mgr.mountFn = defaultRootfsMount
	}
	if mgr.unmountFn == nil {
		mgr.unmountFn = defaultRootfsUnmount
	}
	if mgr.mountedFn == nil {
		mgr.mountedFn = defaultMounted
	}

	for _, dir := range []string{mgr.mountsDir, mgr.lowersDir, mgr.supportDir, mgr.recordsDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("failed to create %s: %w", dir, err)
		}
	}
	if err := rootfssupport.Ensure(mgr.supportDir); err != nil {
		return nil, fmt.Errorf("failed to prepare runtime support dir %s: %w", mgr.supportDir, err)
	}
	if err := mgr.recover(); err != nil {
		return nil, err
	}
	return mgr, nil
}

func (m *Manager) Mount(id, imagePath string) (string, error) {
	return m.mount(id, imagePath, true)
}

// EnsureMounted guarantees one resource-level reference without incrementing
// it when the same resource is already active.
func (m *Manager) EnsureMounted(id, imagePath string) (string, error) {
	return m.mount(id, imagePath, false)
}

func (m *Manager) mount(id, imagePath string, addReference bool) (string, error) {
	if err := validateMountID(id); err != nil {
		return "", err
	}
	if imagePath == "" {
		return "", fmt.Errorf("image path is required")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.loadStateLocked(id)
	if err != nil {
		return "", err
	}
	if state != nil {
		if state.refs == 0 {
			state.refs = 1
		} else if addReference {
			state.refs++
		}
		return state.record.MountPath, nil
	}

	mountPath := filepath.Join(m.mountsDir, id)
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		return "", fmt.Errorf("failed to create mount path %s: %w", mountPath, err)
	}
	if err := m.mountFn(imagePath, m.lowerPath(id), mountPath, m.supportDir); err != nil {
		_ = os.RemoveAll(m.lowerPath(id))
		_ = os.RemoveAll(mountPath)
		return "", fmt.Errorf("failed to mount %s as read-only ext4 image: %w", imagePath, err)
	}

	record := Record{
		ID:        id,
		ImagePath: imagePath,
		MountPath: mountPath,
	}
	if err := m.writeRecordLocked(&record); err != nil {
		_ = m.unmountIfMountedLocked(id, mountPath)
		_ = os.RemoveAll(m.lowerPath(id))
		_ = os.RemoveAll(mountPath)
		return "", err
	}

	m.states[id] = &mountState{
		record: record,
		refs:   1,
	}
	return mountPath, nil
}

func (m *Manager) Unmount(id string) (UnmountResult, error) {
	return m.unmount(id, false)
}

// ReleaseResource idempotently releases the resource-level reference.
func (m *Manager) ReleaseResource(id string) (UnmountResult, error) {
	return m.unmount(id, true)
}

func (m *Manager) unmount(id string, missingIsReleased bool) (UnmountResult, error) {
	if err := validateMountID(id); err != nil {
		return UnmountResult{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state, err := m.loadStateLocked(id)
	if err != nil {
		return UnmountResult{}, err
	}
	if state == nil {
		if missingIsReleased {
			return UnmountResult{Released: true}, nil
		}
		return UnmountResult{}, fmt.Errorf("oss rootfs %s is not mounted", id)
	}
	if state.refs > 1 {
		state.refs--
		return UnmountResult{MountPath: state.record.MountPath, Released: false}, nil
	}

	if err := m.unmountIfMountedLocked(state.record.ID, state.record.MountPath); err != nil {
		return UnmountResult{}, err
	}
	if err := m.deleteRecordLocked(id); err != nil {
		return UnmountResult{}, err
	}
	delete(m.states, id)
	if err := os.RemoveAll(m.lowerPath(id)); err != nil && !os.IsNotExist(err) {
		return UnmountResult{}, fmt.Errorf("failed to remove lower path %s: %w", m.lowerPath(id), err)
	}
	if err := os.RemoveAll(state.record.MountPath); err != nil && !os.IsNotExist(err) {
		return UnmountResult{}, fmt.Errorf("failed to remove mount path %s: %w", state.record.MountPath, err)
	}
	return UnmountResult{MountPath: state.record.MountPath, Released: true}, nil
}

func (m *Manager) lowerPath(id string) string {
	return filepath.Join(m.lowersDir, id)
}

func validateMountID(id string) error {
	if strings.TrimSpace(id) == "" {
		return fmt.Errorf("mount id is required")
	}
	if strings.TrimSpace(id) != id {
		return fmt.Errorf("mount id %q must not contain leading or trailing whitespace", id)
	}
	if id == "." || id == ".." || filepath.Base(id) != id {
		return fmt.Errorf("mount id %q must be a single path segment", id)
	}
	return nil
}
