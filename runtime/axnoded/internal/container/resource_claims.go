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
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

type OccupiedResource struct {
	ID        string
	Resources map[resourcemanager.ResourceName]string
}

func (m *Manager) ReserveContainerID() (string, error) {
	return m.idGenerator.GetID()
}

func (m *Manager) ReleaseContainerID(id string) {
	if id != "" {
		m.idGenerator.ReleaseId(id)
	}
}

// Occupy Generate a new unique container ID.
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

	if opts.ContainerID == "" {
		opts.ContainerID, err = m.idGenerator.GetID()
		if err != nil {
			return resource, err
		}
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

func (or OccupiedResource) ToLabels() map[string]string {
	annotations := make(map[string]string)
	for r, key := range or.Resources {
		annotations[resourcemanager.ResourceAnnotationKeyPrefix+string(r)] = key
	}
	return annotations
}

func (m *Manager) Release(resource OccupiedResource) error {
	if err := m.ReleaseResource(resource.Resources); err != nil {
		return err
	}
	m.idGenerator.ReleaseId(resource.ID)
	return nil
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
	if c, ok := m.containers.Get(id); ok && c != nil && c.Spec != nil {
		resource := collectResourceFromSpec(id, c.Spec)
		logrus.Debugf("collect resource for %s success, details: %+v", id, resource.Resources)
		return resource, nil
	}

	oci, err := runtimeoci.LoadSpec(filepath.Join(m.root, id, "config.json"))
	if err != nil {
		return OccupiedResource{}, err
	}
	resource := collectResourceFromSpec(id, oci)
	logrus.Debugf("collect resource for %s success, details: %+v", id, resource.Resources)
	return resource, nil
}

func collectResourceFromSpec(id string, oci *specs.Spec) OccupiedResource {
	resource := OccupiedResource{
		ID:        id,
		Resources: make(map[resourcemanager.ResourceName]string),
	}
	if oci == nil {
		return resource
	}
	for resourceName, key := range oci.Annotations {
		if after, ok := strings.CutPrefix(resourceName, resourcemanager.ResourceAnnotationKeyPrefix); ok {
			name := resourcemanager.ResourceName(after)
			if isManagedResourceClaim(name) {
				resource.Resources[name] = key
			}
		}
	}

	if _, ok := resource.Resources[resourcemanager.CgroupResourceName]; !ok {
		if oci.Linux != nil && oci.Linux.CgroupsPath != "" {
			resource.Resources[resourcemanager.CgroupResourceName] = oci.Linux.CgroupsPath
		}
	}
	return resource
}

// The resource annotation namespace also contains persisted runtime contracts,
// such as ephemeral-storage reservation metadata. Only pool-backed claims belong
// to the generic resource managers and may be recycled through Manager.Release.
func isManagedResourceClaim(name resourcemanager.ResourceName) bool {
	switch name {
	case resourcemanager.CgroupResourceName, resourcemanager.InterfaceResourceName:
		return true
	default:
		return false
	}
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
