package container

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/fileutil"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

func (m *Manager) StoreMetadata(id string, data *apipb.ContainerMetadata) error {
	if data == nil || data.GetID() == "" || id == "" || data.GetID() != id {
		return fmt.Errorf("container metadata id %q does not match storage id %q", data.GetID(), id)
	}
	containerRoot := filepath.Join(m.root, id)
	start := time.Now()

	if _, err := os.Stat(containerRoot); os.IsNotExist(err) {
		if err := os.MkdirAll(containerRoot, 0755); err != nil {
			return fmt.Errorf("create container root %s: %w", containerRoot, err)
		}
	} else if err != nil {
		return fmt.Errorf("stat container root %s: %w", containerRoot, err)
	}
	dataFile := filepath.Join(containerRoot, config.ContainerMetaFile)
	bytes, err := proto.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal container %s metadata: %w", data.ID, err)
	}

	if err := fileutil.AtomicWriteFile(dataFile, bytes, 0600); err != nil {
		return fmt.Errorf("save container %s metadata: %w", data.ID, err)
	}
	if current, ok := m.containers.Get(id); ok && current != nil {
		replacement := *current
		replacement.Metadata = proto.Clone(data).(*apipb.ContainerMetadata)
		m.containers.Set(id, &replacement)
	} else {
		container, err := m.loadContainer(containerRoot)
		if err != nil {
			return fmt.Errorf("load persisted container %s metadata: %w", id, err)
		}
		m.containers.Set(id, container)
	}
	logrus.Debugf("store container %s metadata success, cost %v", data.ID, time.Since(start).String())
	return nil
}

func (m *Manager) loadContainer(containerRoot string) (*Container, error) {
	b, err := os.ReadFile(filepath.Join(containerRoot, config.ContainerMetaFile))
	if err != nil {
		return nil, err
	}

	var meta apipb.ContainerMetadata
	if err = proto.Unmarshal(b, &meta); err != nil {
		return nil, err
	}
	directoryID := filepath.Base(containerRoot)
	if meta.GetID() == "" || meta.GetID() != directoryID {
		return nil, fmt.Errorf("container metadata id %q does not match directory %q", meta.GetID(), directoryID)
	}

	container := new(Container)
	container.Metadata = &meta
	container.PATH = containerRoot
	container.Status, err = LoadStatus(containerRoot)
	if err != nil {
		logrus.Warnf("load status for container %s failed: %v", container.Metadata.ID, err)
		return nil, err
	}

	container.Spec = new(spec.Spec)
	specByte, err := os.ReadFile(filepath.Join(containerRoot, config.ContainerSpecFile))
	if err != nil {
		logrus.Warnf("load spec for container %s failed: %v", container.Metadata.ID, err)
		return container, nil
	}
	if err = json.Unmarshal(specByte, container.Spec); err != nil {
		logrus.Warnf("load spec for container %s failed: %v", container.Metadata.ID, err)
		return container, nil
	}

	return container, nil
}

func (m *Manager) loadContainers() error {
	list, err := os.ReadDir(m.root)
	if err != nil {
		logrus.Debugf("read dir %s failed: %v", m.root, err)
		if errors.Is(err, os.ErrNotExist) {
			if err = os.MkdirAll(m.root, 0755); err != nil {
				return err
			}
		} else {
			return err
		}
	}
	logrus.Debugf("manager loaded containers under %s", m.root)

	m.containers = cmap.New[*Container]()
	for _, containerDir := range list {
		if !containerDir.IsDir() {
			logrus.Debugf("manager load skip %s", containerDir.Name())
			continue
		}
		container, err := m.loadContainer(filepath.Join(m.root, containerDir.Name()))
		if err != nil {
			logrus.Errorf("load container %s failed: %v", containerDir.Name(), err)
			continue
		}
		m.containers.Set(containerDir.Name(), container)
	}
	return nil
}
