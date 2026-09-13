package container

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/sirupsen/logrus"
)

// UpdateFunc is function used to update the container status. If there
// is an error, the update will be rolled back.
type UpdateFunc func(Status) (Status, error)

// StatusStorage manages the container status with a storage backend.
type StatusStorage interface {
	// Get a container status.
	Get() Status
	// UpdateSync updates the container status and the on disk checkpoint.
	// Note that the update MUST be applied in one transaction.
	UpdateSync(UpdateFunc) error
	// Update the container status. Note that the update MUST be applied
	// in one transaction.
	Update(UpdateFunc) error
	// Delete the container status.
	// Note:
	// * Delete should be idempotent.
	// * The status must be deleted in one transaction.
	Delete() error
}

// LoadStatus loads container status from checkpoint. There shouldn't be threads
// writing to the file during loading.
func LoadStatus(containerRoot string) (StatusStorage, error) {
	path := filepath.Join(containerRoot, config.ContainerStatusFile)
	data, err := os.ReadFile(path)
	if err != nil {
		// Missing lifecycle evidence is unknown. Runtime inventory may enrich the
		// live identity later, but file absence cannot manufacture Running state.
		if os.IsNotExist(err) {
			return &statusStorage{
				path: path,
			}, nil
		}
		return nil, fmt.Errorf("failed to read status from %q: %w", path, err)
	}
	var status Status
	if err := status.decode(data); err != nil {
		return nil, fmt.Errorf("failed to decode status %q: %w", data, err)
	}
	return &statusStorage{
		path:   path,
		status: status,
	}, nil
}

type statusStorage struct {
	sync.RWMutex
	path   string
	status Status
}

// Get a copy of container status.
func (s *statusStorage) Get() Status {
	s.RLock()
	defer s.RUnlock()
	// Deep copy is needed in case some fields in Status are updated after Get()
	// is called.
	return deepCopyOf(s.status)
}

func deepCopyOf(s Status) Status {
	return s
}

// UpdateSync updates the container status and the on disk checkpoint.
func (s *statusStorage) UpdateSync(u UpdateFunc) error {
	s.Lock()
	defer s.Unlock()
	newStatus, err := u(s.status)
	if err != nil {
		return err
	}

	// Don't do update if the new status equal with the old status
	if s.status.Equal(newStatus) {
		return nil
	}

	data, err := newStatus.encode()
	if err != nil {
		return fmt.Errorf("failed to encode status: %w", err)
	}

	if err := Os().WriteFile(s.path, data, 0600); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			logrus.Debugf("skip checkpoint for removed status path %q", s.path)
			s.status = newStatus
			return nil
		}
		return fmt.Errorf("failed to checkpoint status to %q: %w", s.path, err)
	}

	logrus.Debugf("updateSync changed new status: %+v, old status: %+v", newStatus, s.status)

	s.status = newStatus
	return nil
}

// Update the container status.
func (s *statusStorage) Update(u UpdateFunc) error {
	s.Lock()
	defer s.Unlock()
	newStatus, err := u(s.status)
	if err != nil {
		return err
	}
	s.status = newStatus
	return nil
}

// Delete deletes the container status from disk atomically.
func (s *statusStorage) Delete() error {
	temp := filepath.Dir(s.path) + ".del-" + filepath.Base(s.path)
	if err := os.Rename(s.path, temp); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.RemoveAll(temp)
}
