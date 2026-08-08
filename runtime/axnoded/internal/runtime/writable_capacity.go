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

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
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
	available := int64(stat.Bavail) * int64(stat.Bsize)
	committed := int64(0)
	for _, reservation := range m.reservations {
		committed += reservation.RequestBytes
	}
	if requestBytes > available-m.systemReserve || requestBytes > (int64(stat.Blocks)*int64(stat.Bsize))-m.systemReserve-committed {
		metrics.RecordWritableLayerOperation(runtimeName, "reserve", "insufficient_capacity")
		return fmt.Errorf("insufficient writable layer capacity: request=%d available=%d system_reserve=%d committed=%d", requestBytes, available, m.systemReserve, committed)
	}
	projectID := uint32(0)
	if runtimeName == "runc" {
		projectID = m.allocateProjectID(containerID)
	}
	reservation := writableReservation{ContainerID: containerID, RuntimeName: runtimeName, RequestBytes: requestBytes, LimitBytes: limitBytes, ProjectID: projectID, CreatedAt: time.Now().UTC()}
	if err := writeJSONAtomic(m.dir, containerID+".json", reservation); err != nil {
		metrics.RecordWritableLayerOperation(runtimeName, "reserve", "persistence_failure")
		return err
	}
	m.reservations[containerID] = reservation
	metrics.RecordWritableLayerOperation(runtimeName, "reserve", "success")
	return nil
}

func (m *writableCapacityManager) allocateProjectID(containerID string) uint32 {
	used := make(map[uint32]struct{}, len(m.reservations))
	for _, reservation := range m.reservations {
		if reservation.ProjectID != 0 {
			used[reservation.ProjectID] = struct{}{}
		}
	}
	id := uint32(10000) + crc32.ChecksumIEEE([]byte(containerID))%2000000000
	for {
		if _, exists := used[id]; !exists {
			return id
		}
		id++
	}
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
		value := document.Annotations["io.axnoded.resource/writable-layer"]
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
		metrics.RecordWritableLayerOperation(reservation.RuntimeName, "release", "failure")
		return fmt.Errorf("remove writable reservation: %w", err)
	}
	delete(m.reservations, containerID)
	if err := syncDir(m.dir); err != nil {
		metrics.RecordWritableLayerOperation(reservation.RuntimeName, "release", "failure")
		return err
	}
	metrics.RecordWritableLayerOperation(reservation.RuntimeName, "release", "success")
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
	tmp, err := os.CreateTemp(dir, ".reservation-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Sync(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return err
	}
	if err := syncDir(dir); err != nil {
		return err
	}
	ok = true
	return nil
}

func syncDir(dir string) error {
	file, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}
