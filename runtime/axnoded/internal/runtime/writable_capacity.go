package runtime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/durablefile"
	"golang.org/x/sys/unix"
)

type writableCharge struct {
	ContainerID  string    `json:"container_id"`
	RequestBytes int64     `json:"request_bytes"`
	LimitBytes   int64     `json:"limit_bytes"`
	CreatedAt    time.Time `json:"created_at"`
}

type writableCapacityManager struct {
	mu            sync.Mutex
	dir           string
	systemReserve int64
	charges       map[string]writableCharge
}

var writableCapacityManagers sync.Map

func sharedWritableCapacityManager(filestoreDir string, systemReserve int64) (*writableCapacityManager, error) {
	if filestoreDir == "" {
		return nil, nil
	}
	key := filepath.Clean(filestoreDir)
	if existing, ok := writableCapacityManagers.Load(key); ok {
		manager := existing.(*writableCapacityManager)
		if manager.systemReserve != systemReserve {
			return nil, fmt.Errorf("filestore %s has conflicting system reserve: %d != %d", key, manager.systemReserve, systemReserve)
		}
		return manager, nil
	}
	manager := &writableCapacityManager{
		dir: filepath.Join(key, "allocation-charges"), systemReserve: systemReserve,
		charges: make(map[string]writableCharge),
	}
	if err := manager.load(); err != nil {
		return nil, err
	}
	actual, loaded := writableCapacityManagers.LoadOrStore(key, manager)
	if loaded {
		return actual.(*writableCapacityManager), nil
	}
	return manager, nil
}

func (m *writableCapacityManager) load() error {
	if err := os.MkdirAll(m.dir, 0700); err != nil {
		return fmt.Errorf("create writable charge directory: %w", err)
	}
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return fmt.Errorf("read writable charges: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.dir, entry.Name()))
		if err != nil {
			return fmt.Errorf("read writable charge %s: %w", entry.Name(), err)
		}
		var charge writableCharge
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&charge); err != nil {
			return fmt.Errorf("decode writable charge %s: %w", entry.Name(), err)
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			return fmt.Errorf("decode writable charge %s: trailing JSON", entry.Name())
		}
		expectedName := charge.ContainerID + ".json"
		if !validPersistentContainerID(charge.ContainerID) || entry.Name() != expectedName || charge.RequestBytes <= 0 || charge.LimitBytes < charge.RequestBytes {
			return fmt.Errorf("invalid writable charge %s", entry.Name())
		}
		if _, exists := m.charges[charge.ContainerID]; exists {
			return fmt.Errorf("duplicate writable charge for container %s", charge.ContainerID)
		}
		m.charges[charge.ContainerID] = charge
	}
	return nil
}

func (m *writableCapacityManager) Charge(containerID, runtimeName string, requestBytes, limitBytes int64) error {
	if m == nil || requestBytes == 0 {
		return nil
	}
	if !validPersistentContainerID(containerID) {
		return fmt.Errorf("invalid container ID for writable charge %q", containerID)
	}
	if requestBytes <= 0 || limitBytes < requestBytes {
		return fmt.Errorf("invalid writable charge: request=%d limit=%d", requestBytes, limitBytes)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.charges[containerID]; ok {
		if existing.RequestBytes == requestBytes && existing.LimitBytes == limitBytes {
			return nil
		}
		return fmt.Errorf("container %s already has a different writable charge", containerID)
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(filepath.Dir(m.dir), &stat); err != nil {
		return fmt.Errorf("stat writable filestore: %w", err)
	}
	available := hostlinux.StatfsBytes(uint64(stat.Bavail), int64(stat.Bsize))
	capacity := hostlinux.StatfsBytes(uint64(stat.Blocks), int64(stat.Bsize))
	committed := int64(0)
	for _, charge := range m.charges {
		committed = hostlinux.SaturatingAdd(committed, charge.RequestBytes)
	}
	if requestBytes > hostlinux.RemainingCapacity(available, m.systemReserve) || requestBytes > hostlinux.RemainingCapacity(capacity, m.systemReserve, committed) {
		metrics.RecordEphemeralStorageOperation(runtimeName, "reserve", "insufficient_capacity")
		return fmt.Errorf("insufficient ephemeral storage capacity: request=%d available=%d system_reserve=%d committed=%d", requestBytes, available, m.systemReserve, committed)
	}
	charge := writableCharge{ContainerID: containerID, RequestBytes: requestBytes, LimitBytes: limitBytes, CreatedAt: time.Now().UTC()}
	if err := writeJSONAtomic(m.dir, containerID+".json", charge); err != nil {
		metrics.RecordEphemeralStorageOperation(runtimeName, "reserve", "persistence_failure")
		return err
	}
	m.charges[containerID] = charge
	metrics.RecordEphemeralStorageOperation(runtimeName, "reserve", "success")
	return nil
}

func (m *writableCapacityManager) Reconcile(retained map[string]struct{}, cleanup func(string) error) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	stale := make([]string, 0)
	for id := range m.charges {
		if _, ok := retained[id]; !ok {
			stale = append(stale, id)
		}
	}
	m.mu.Unlock()
	sort.Strings(stale)

	var result error
	for _, id := range stale {
		if cleanup != nil {
			if err := cleanup(id); err != nil {
				result = errors.Join(result, fmt.Errorf("cleanup stale ephemeral storage charge %s: %w", id, err))
				continue
			}
		}
		if err := m.Release(id); err != nil {
			result = errors.Join(result, fmt.Errorf("release stale writable charge %s: %w", id, err))
		}
	}
	return result
}

func (m *writableCapacityManager) Release(containerID string) error {
	if m == nil || containerID == "" {
		return nil
	}
	if !validPersistentContainerID(containerID) {
		return fmt.Errorf("invalid container ID for writable charge %q", containerID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.charges[containerID]
	if !ok {
		return nil
	}
	if err := os.Remove(filepath.Join(m.dir, containerID+".json")); err != nil && !os.IsNotExist(err) {
		metrics.RecordEphemeralStorageOperation("runsc", "release", "failure")
		return fmt.Errorf("remove writable charge: %w", err)
	}
	delete(m.charges, containerID)
	if err := durablefile.SyncDir(m.dir); err != nil {
		metrics.RecordEphemeralStorageOperation("runsc", "release", "failure")
		return err
	}
	metrics.RecordEphemeralStorageOperation("runsc", "release", "success")
	return nil
}

func validPersistentContainerID(value string) bool {
	if value == "" || value == "." || filepath.Base(value) != value {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '.' || character == '_' || character == '-' || character == '+' {
			continue
		}
		return false
	}
	return true
}

func writeJSONAtomic(dir, name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return durablefile.Write(filepath.Join(dir, name), data, 0600)
}
