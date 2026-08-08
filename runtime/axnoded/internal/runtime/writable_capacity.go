package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/hostlinux"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/durablefile"
	"golang.org/x/sys/unix"
)

type writableReservation struct {
	ContainerID  string    `json:"container_id"`
	RuntimeName  string    `json:"runtime_name"`
	RequestBytes int64     `json:"request_bytes"`
	LimitBytes   int64     `json:"limit_bytes"`
	ProjectID    uint32    `json:"project_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type writableCapacityManager struct {
	mu            sync.Mutex
	dir           string
	systemReserve int64
	reservations  map[string]writableReservation
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
		dir: filepath.Join(key, "reservations"), systemReserve: systemReserve,
		reservations: make(map[string]writableReservation),
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
		return fmt.Errorf("create writable reservation directory: %w", err)
	}
	entries, err := os.ReadDir(m.dir)
	if err != nil {
		return fmt.Errorf("read writable reservations: %w", err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(m.dir, entry.Name()))
		if err != nil {
			return fmt.Errorf("read writable reservation %s: %w", entry.Name(), err)
		}
		var reservation writableReservation
		if err := json.Unmarshal(data, &reservation); err != nil {
			return fmt.Errorf("decode writable reservation %s: %w", entry.Name(), err)
		}
		expectedName := reservation.ContainerID + ".json"
		if !validPersistentContainerID(reservation.ContainerID) || entry.Name() != expectedName || reservation.RequestBytes <= 0 || reservation.LimitBytes < reservation.RequestBytes {
			return fmt.Errorf("invalid writable reservation %s", entry.Name())
		}
		if _, exists := m.reservations[reservation.ContainerID]; exists {
			return fmt.Errorf("duplicate writable reservation for container %s", reservation.ContainerID)
		}
		m.reservations[reservation.ContainerID] = reservation
	}
	return nil
}

func (m *writableCapacityManager) Reserve(containerID, runtimeName string, requestBytes, limitBytes int64) error {
	if m == nil || requestBytes == 0 {
		return nil
	}
	if !validPersistentContainerID(containerID) {
		return fmt.Errorf("invalid container ID for writable reservation %q", containerID)
	}
	if requestBytes <= 0 || limitBytes < requestBytes {
		return fmt.Errorf("invalid writable reservation: request=%d limit=%d", requestBytes, limitBytes)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.reservations[containerID]; ok {
		if existing.RequestBytes == requestBytes && existing.LimitBytes == limitBytes && existing.RuntimeName == runtimeName {
			return nil
		}
		return fmt.Errorf("container %s already has a different writable reservation", containerID)
	}
	var stat unix.Statfs_t
	if err := unix.Statfs(filepath.Dir(m.dir), &stat); err != nil {
		return fmt.Errorf("stat writable filestore: %w", err)
	}
	available := hostlinux.StatfsBytes(uint64(stat.Bavail), int64(stat.Bsize))
	capacity := hostlinux.StatfsBytes(uint64(stat.Blocks), int64(stat.Bsize))
	committed := int64(0)
	for _, reservation := range m.reservations {
		committed = hostlinux.SaturatingAdd(committed, reservation.RequestBytes)
	}
	if requestBytes > hostlinux.RemainingCapacity(available, m.systemReserve) || requestBytes > hostlinux.RemainingCapacity(capacity, m.systemReserve, committed) {
		metrics.RecordEphemeralStorageOperation(runtimeName, "reserve", "insufficient_capacity")
		return fmt.Errorf("insufficient writable layer capacity: request=%d available=%d system_reserve=%d committed=%d", requestBytes, available, m.systemReserve, committed)
	}
	projectID := uint32(0)
	if runtimeName == "runc" {
		allocatedProjectID, err := m.allocateProjectID(containerID)
		if err != nil {
			return err
		}
		projectID = allocatedProjectID
	}
	reservation := writableReservation{ContainerID: containerID, RuntimeName: runtimeName, RequestBytes: requestBytes, LimitBytes: limitBytes, ProjectID: projectID, CreatedAt: time.Now().UTC()}
	if err := writeJSONAtomic(m.dir, containerID+".json", reservation); err != nil {
		metrics.RecordEphemeralStorageOperation(runtimeName, "reserve", "persistence_failure")
		return err
	}
	m.reservations[containerID] = reservation
	metrics.RecordEphemeralStorageOperation(runtimeName, "reserve", "success")
	return nil
}

func (m *writableCapacityManager) allocateProjectID(containerID string) (uint32, error) {
	used := make(map[uint32]struct{}, len(m.reservations))
	for _, reservation := range m.reservations {
		if reservation.ProjectID != 0 {
			used[reservation.ProjectID] = struct{}{}
		}
	}
	rangeSize := uint64(hostlinux.AllocationProjectIDMax) - uint64(hostlinux.AllocationProjectIDMin) + 1
	if uint64(len(used)) >= rangeSize {
		return 0, fmt.Errorf("XFS project ID range is exhausted")
	}
	id := hostlinux.AllocationProjectIDMin + uint32(uint64(crc32.ChecksumIEEE([]byte(containerID)))%rangeSize)
	for attempts := uint64(0); attempts < rangeSize; attempts++ {
		if _, exists := used[id]; !exists {
			return id, nil
		}
		if id == hostlinux.AllocationProjectIDMax {
			id = hostlinux.AllocationProjectIDMin
		} else {
			id++
		}
	}
	return 0, fmt.Errorf("XFS project ID range is exhausted")
}

func (m *writableCapacityManager) ProjectID(containerID string) uint32 {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.reservations[containerID].ProjectID
}

func (m *writableCapacityManager) ReconcileRuntime(runtimeName string, retained map[string]struct{}, cleanup func(string) error) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	stale := make([]string, 0)
	for id, reservation := range m.reservations {
		if reservation.RuntimeName != runtimeName {
			continue
		}
		if _, ok := retained[id]; !ok {
			stale = append(stale, id)
		}
	}
	m.mu.Unlock()

	var result error
	for _, id := range stale {
		if cleanup != nil {
			if err := cleanup(id); err != nil {
				result = errors.Join(result, fmt.Errorf("cleanup stale writable layer %s: %w", id, err))
				continue
			}
		}
		if err := m.Release(id); err != nil {
			result = errors.Join(result, fmt.Errorf("release stale writable reservation %s: %w", id, err))
		}
	}
	return result
}

func (m *writableCapacityManager) ValidateRuntimeReservations(runtimeName, containerRoot string, retained map[string]struct{}) error {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	expected := make([]writableReservation, 0)
	for id, reservation := range m.reservations {
		if reservation.RuntimeName == runtimeName {
			if _, ok := retained[id]; ok {
				expected = append(expected, reservation)
			}
		}
	}
	m.mu.Unlock()

	var result error
	for _, reservation := range expected {
		data, err := os.ReadFile(filepath.Join(containerRoot, reservation.ContainerID, "config.json"))
		if err != nil {
			result = errors.Join(result, fmt.Errorf("writable reservation %s has no recoverable OCI spec: %w", reservation.ContainerID, err))
			continue
		}
		var document struct {
			Annotations map[string]string `json:"annotations"`
		}
		if err := json.Unmarshal(data, &document); err != nil {
			result = errors.Join(result, fmt.Errorf("decode OCI spec for writable reservation %s: %w", reservation.ContainerID, err))
			continue
		}
		var annotation struct {
			RequestBytes int64 `json:"request_bytes"`
			LimitBytes   int64 `json:"limit_bytes"`
		}
		value := document.Annotations["io.axnoded.resource/ephemeral-storage"]
		if value == "" || json.Unmarshal([]byte(value), &annotation) != nil || annotation.RequestBytes != reservation.RequestBytes || annotation.LimitBytes != reservation.LimitBytes {
			result = errors.Join(result, fmt.Errorf("writable reservation %s does not match its OCI annotation", reservation.ContainerID))
		}
	}
	return result
}

func (m *writableCapacityManager) Release(containerID string) error {
	if m == nil || containerID == "" {
		return nil
	}
	if !validPersistentContainerID(containerID) {
		return fmt.Errorf("invalid container ID for writable reservation %q", containerID)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	reservation, ok := m.reservations[containerID]
	if !ok {
		return nil
	}
	if err := os.Remove(filepath.Join(m.dir, containerID+".json")); err != nil && !os.IsNotExist(err) {
		metrics.RecordEphemeralStorageOperation(reservation.RuntimeName, "release", "failure")
		return fmt.Errorf("remove writable reservation: %w", err)
	}
	delete(m.reservations, containerID)
	if err := durablefile.SyncDir(m.dir); err != nil {
		metrics.RecordEphemeralStorageOperation(reservation.RuntimeName, "release", "failure")
		return err
	}
	metrics.RecordEphemeralStorageOperation(reservation.RuntimeName, "release", "success")
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
