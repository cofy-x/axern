package container

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
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
		// init status with Running
		if os.IsNotExist(err) {
			status := Status{
				Unknown:       false,
				StartedAt:     time.Now().Format(time.RFC3339),
				ExitCodeKnown: false,
			}
			hydrateStatusPIDFromRuntimeFile(&status, containerRoot)
			return &statusStorage{
				path:   path,
				status: status,
			}, nil
		}
		return nil, fmt.Errorf("failed to read status from %q: %w", path, err)
	}
	var status Status
	if err := status.decode(data); err != nil {
		return nil, fmt.Errorf("failed to decode status %q: %w", data, err)
	}
	hydrateStatusPIDFromRuntimeFile(&status, containerRoot)
	return &statusStorage{
		path:   path,
		status: status,
	}, nil
}

func hydrateStatusPIDFromRuntimeFile(status *Status, containerRoot string) {
	if status == nil || status.Pid > 0 || status.FinishedAt != "" {
		return
	}
	pidData, err := os.ReadFile(filepath.Join(containerRoot, "runtime.pid"))
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil || pid <= 0 {
		return
	}
	status.Pid = pid
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
	copy := s
	if s.ResourceSpec != nil {
		copy.ResourceSpec = proto.Clone(s.ResourceSpec).(*commonv1.ResourceSpec)
	}
	// LinuxResources is a pointer, and therefore needs
	// a manual deep copy.
	// This will need updates when new fields are added to ContainerResources.
	if s.LinuxResources == nil {
		return copy
	}
	copy.LinuxResources = &runtime.LinuxContainerResources{}
	if s.LinuxResources != nil {
		hugepageLimits := make([]*runtime.HugepageLimit, 0, len(s.LinuxResources.HugepageLimits))
		for _, l := range s.LinuxResources.HugepageLimits {
			if l != nil {
				hugepageLimits = append(hugepageLimits, &runtime.HugepageLimit{
					PageSize: l.PageSize,
					Limit:    l.Limit,
				})
			}
		}
		copy.LinuxResources = &runtime.LinuxContainerResources{
			CpuPeriod:              s.LinuxResources.CpuPeriod,
			CpuQuota:               s.LinuxResources.CpuQuota,
			CpuShares:              s.LinuxResources.CpuShares,
			CpusetCpus:             s.LinuxResources.CpusetCpus,
			CpusetMems:             s.LinuxResources.CpusetMems,
			MemoryLimitInBytes:     s.LinuxResources.MemoryLimitInBytes,
			MemorySwapLimitInBytes: s.LinuxResources.MemorySwapLimitInBytes,
			OomScoreAdj:            s.LinuxResources.OomScoreAdj,
			Unified:                s.LinuxResources.Unified,
			HugepageLimits:         hugepageLimits,
		}
	}
	return copy
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
