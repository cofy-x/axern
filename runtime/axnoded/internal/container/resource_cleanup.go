package container

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/jsonutil"
	"github.com/sirupsen/logrus"
)

func (m *Manager) CleanContainerRoot(id string) {
	if err := os.RemoveAll(filepath.Join(m.root, id)); err != nil {
		if strings.Contains(err.Error(), "directory not empty") {
			err = os.RemoveAll(filepath.Join(m.root, id))
			if err != nil {
				logrus.Warnf("remove container %s root failed: %v", filepath.Join(m.root, id), err)
			}
		}
	}
}

func (m *Manager) Delete(id string) error {
	resource, err := m.CollectResourceByID(id)
	if err != nil {
		return fmt.Errorf("collect resource for %s: %w", id, err)
	}
	if err := m.Release(resource); err != nil {
		return fmt.Errorf("release resources for %s: %w", id, err)
	}

	m.CleanContainerRoot(id)
	if !m.containers.Has(id) {
		return nil
	}
	m.containers.Remove(id)
	m.stopMonitor(id)
	return nil
}

func (m *Manager) ReleaseContainerResources(id string) error {
	resource, err := m.CollectResourceByID(id)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !m.containers.Has(id) {
			return nil
		}
		return fmt.Errorf("collect resource for %s before release: %w", id, err)
	}
	if len(resource.Resources) == 0 {
		return nil
	}

	if err := m.ReleaseResource(resource.Resources); err != nil {
		return err
	}
	if err := m.clearContainerResourceClaims(id); err != nil {
		return fmt.Errorf("clear resource claims for %s: %w", id, err)
	}
	return nil
}

func (m *Manager) clearContainerResourceClaims(id string) error {
	ociPath := filepath.Join(m.root, id, config.ContainerSpecFile)
	oci, err := runtimeoci.LoadSpec(ociPath)
	if err == nil {
		clearSpecResourceClaims(oci.Annotations)
		if oci.Linux != nil {
			oci.Linux.CgroupsPath = ""
		}
		buf, _ := jsonutil.UnescapedMarshal(oci)
		if err := os.WriteFile(ociPath, buf, 0644); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if c, ok := m.containers.Get(id); ok && c != nil && c.Spec != nil {
		clearSpecResourceClaims(c.Spec.Annotations)
		if c.Spec.Linux != nil {
			c.Spec.Linux.CgroupsPath = ""
		}
	}
	return nil
}

func clearSpecResourceClaims(annotations map[string]string) {
	for key := range annotations {
		if strings.HasPrefix(key, resourcemanager.ResourceAnnotationKeyPrefix) {
			delete(annotations, key)
		}
	}
}
