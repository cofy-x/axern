package container

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/sirupsen/logrus"
)

type OccupiedResource struct {
	ID        string
	Resources map[resourcemanager.ResourceName]string
}

// Occupy reserves node-local resources for an explicit Allocation identity.
func (m *Manager) Occupy(opts resourcemanager.AllocateOption, resources ...resourcemanager.ResourceName) (resource OccupiedResource, err error) {
	start := time.Now()
	defer func() {
		if err == nil {
			logrus.Debugf("occupy resource %+v success, cost %v", resources, time.Since(start))
		}
	}()
	if m.containers.Count() >= MaxContainerNum {
		return resource, fmt.Errorf("container limit %d reached: %w", MaxContainerNum, errord.ErrResourceExhausted)
	}

	if strings.TrimSpace(opts.ContainerID) == "" {
		return resource, fmt.Errorf("resource allocation requires an explicit allocation id")
	}
	resource.ID = opts.ContainerID
	resource.Resources = make(map[resourcemanager.ResourceName]string)

	allocatedResource := make(map[resourcemanager.ResourceName]string)
	defer func() {
		if err != nil {
			if cleanupErr := m.ReleaseResource(allocatedResource); cleanupErr != nil {
				err = errors.Join(err, fmt.Errorf("rollback allocated resources: %w", cleanupErr))
			}
		}
	}()
	for _, r := range resources {
		manager, ok := m.resourceManagers.Get(string(r))
		if !ok {
			return resource, fmt.Errorf("resource %s not found", r)
		}
		allocateStarted := time.Now()
		key, allocErr := manager.Allocate(opts)
		result := "ok"
		if allocErr != nil {
			result = "error"
		}
		metrics.RecordResourceAllocateStage(string(r), "total", result, time.Since(allocateStarted).Seconds())
		if allocErr != nil {
			err = fmt.Errorf("allocate resource %v failed: %w", r, allocErr)
			return resource, err
		}
		logrus.Debugf("allocate resource %v success, cost %v", r, time.Since(allocateStarted).String())
		resource.Resources[r] = key.ToString()
		allocatedResource[r] = key.ToString()
	}

	return resource, nil
}

func (m *Manager) Release(resource OccupiedResource) error {
	return m.ReleaseResource(resource.Resources)
}

func (m *Manager) ReleaseResource(resources map[resourcemanager.ResourceName]string) error {
	// Cgroup retirement is the memory-commitment release barrier. Keep it last:
	// if any network or other allocation-owned resource cannot be retired, the
	// cgroup lease must remain assigned so node-local admission continues to
	// charge the allocation. Sorting also makes cleanup and its diagnostics
	// deterministic instead of depending on Go map iteration order.
	names := make([]resourcemanager.ResourceName, 0, len(resources))
	for name := range resources {
		if name != resourcemanager.CgroupResourceName {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })

	release := func(r resourcemanager.ResourceName) error {
		key := resources[r]
		manager, ok := m.resourceManagers.Get(string(r))
		if !ok {
			return fmt.Errorf("resource manager for %s not found", r)
		}
		if err := manager.Recycle(key); err != nil {
			return fmt.Errorf("recycle resource %s[%s]: %w", r, key, err)
		}
		logrus.Infof("recycle resource %s[%s] success", r, key)
		return nil
	}

	var releaseErr error
	for _, name := range names {
		releaseErr = errors.Join(releaseErr, release(name))
	}
	if releaseErr != nil {
		return releaseErr
	}
	if _, ok := resources[resourcemanager.CgroupResourceName]; ok {
		return release(resourcemanager.CgroupResourceName)
	}
	return nil
}

func (m *Manager) CollectResourceByID(id string) (OccupiedResource, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return OccupiedResource{}, errord.ErrInvalidArgument
	}
	resource := OccupiedResource{ID: id, Resources: make(map[resourcemanager.ResourceName]string)}
	for item := range m.resourceManagers.IterBuffered() {
		if key, ok := item.Val.AllocationResource(id); ok {
			resource.Resources[resourcemanager.ResourceName(item.Key)] = key
		}
	}
	logrus.Debugf("collect resource for %s success, details: %+v", id, resource.Resources)
	return resource, nil
}

func (m *Manager) RuntimeCgroupPath(containerID string) (string, error) {
	oci, err := runtimeoci.LoadSpec(filepath.Join(m.root, containerID, "config.json"))
	if err != nil {
		return "", err
	}
	if oci.Linux != nil && oci.Linux.CgroupsPath != "" {
		return oci.Linux.CgroupsPath, nil
	}

	resource, err := m.CollectResourceByID(containerID)
	if err != nil {
		return "", err
	}
	cgroupPath, ok := resource.Resources[resourcemanager.CgroupResourceName]
	if !ok || cgroupPath == "" {
		cgroupPath, err = m.runtimeCgroupPathFromPIDFile(containerID)
		if err != nil {
			return "", fmt.Errorf("cgroup path not found for container %s", containerID)
		}
	}
	return cgroupPath, nil
}

func (m *Manager) AllocationCgroupPath(allocationID string) (string, error) {
	resource, err := m.CollectResourceByID(allocationID)
	if err != nil {
		return "", err
	}
	cgroupPath := strings.TrimSpace(resource.Resources[resourcemanager.CgroupResourceName])
	if cgroupPath == "" {
		return "", fmt.Errorf("allocation %s has no cgroup binding", allocationID)
	}
	return cgroupPath, nil
}

func (m *Manager) runtimeCgroupPathFromPIDFile(containerID string) (string, error) {
	pidBytes, err := os.ReadFile(filepath.Join(m.root, containerID, "runtime.pid"))
	if err != nil {
		return "", err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidBytes)))
	if err != nil {
		return "", err
	}
	return processCgroupPath(pid)
}

func processCgroupPath(pid int) (string, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return "", err
	}
	return parseProcessCgroupPath(string(data))
}

func parseProcessCgroupPath(raw string) (string, error) {
	var fallback string
	for _, line := range strings.Split(strings.TrimSpace(raw), "\n") {
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 || parts[2] == "" {
			continue
		}

		path := filepath.Clean("/" + strings.TrimPrefix(parts[2], "/"))
		switch {
		case parts[0] == "0":
			return path, nil
		case parts[1] == "memory" || strings.Contains(parts[1], "memory"):
			return path, nil
		case fallback == "":
			fallback = path
		}
	}
	if fallback == "" {
		return "", fmt.Errorf("process cgroup path not found")
	}
	return fallback, nil
}
