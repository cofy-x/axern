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
	"github.com/golang/protobuf/proto"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	cmap "github.com/orcaman/concurrent-map/v2"
	"github.com/sirupsen/logrus"
)

func (m *Manager) StoreMetadata(id string, data *apipb.ContainerMetadata) {
	var err error
	containerRoot := filepath.Join(m.root, id)
	start := time.Now()
	defer func() {
		if err == nil && !m.containers.Has(data.ID) {
			if c, err := m.loadContainer(containerRoot); err != nil {
				logrus.Warnf("init loading container %s failed: %v, try later when housekeeping", data.ID, err)
			} else {
				m.containers.Set(data.ID, c)
			}
		}
	}()

	if _, err = os.Stat(containerRoot); os.IsNotExist(err) {
		if err = os.MkdirAll(containerRoot, 0755); err != nil {
			logrus.Errorf("create container root %s failed: %v", containerRoot, err)
			return
		}
	}
	dataFile := filepath.Join(containerRoot, config.ContainerMetaFile)
	bytes, err := proto.Marshal(data)
	if err != nil {
		logrus.Errorf("marshal %s container metadata when store failed: %v", data.ID, err)
		return
	}

	if err = fileutil.AtomicWriteFile(dataFile, bytes, 0600); err != nil {
		logrus.Errorf("save %s container metadata failed: %v", data.ID, err)
		return
	}
	logrus.Debugf("store container %s metadata success, cost %v", data.ID, time.Since(start).String())
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
