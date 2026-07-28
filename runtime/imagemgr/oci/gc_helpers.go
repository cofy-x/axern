package oci

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

type gcAgePolicy uint8

const (
	gcIgnoreAge gcAgePolicy = iota
	gcRequireExpiry
)

func (m *Manager) tryDeleteLayerCandidate(candidate *LayerRecord, now time.Time, ttl time.Duration, agePolicy gcAgePolicy, warningContext string) (bool, error) {
	unlock := m.acquireLayerLock(candidate.Digest)
	defer unlock()

	layer, err := m.store.getLayer(candidate.Digest)
	if err != nil {
		return false, fmt.Errorf("failed to re-read layer metadata %s %s: %w", candidate.Digest, warningContext, err)
	}
	if layer == nil || layer.RefCount != 0 || layer.Path != candidate.Path {
		return false, nil
	}
	if agePolicy == gcRequireExpiry {
		if layer.RefZeroAtUnix == 0 || now.Sub(time.Unix(layer.RefZeroAtUnix, 0)) < ttl {
			return false, nil
		}
	}

	if layer.Path != "" {
		_ = os.RemoveAll(filepath.Dir(layer.Path))
	}
	if err := m.store.deleteLayer(candidate.Digest); err != nil {
		logrus.Warnf("failed to delete layer metadata %s %s: %v", candidate.Digest, warningContext, err)
		return false, nil
	}
	return true, nil
}

func (m *Manager) tryDeleteChainCandidate(candidate *ChainRecord, now time.Time, ttl time.Duration, agePolicy gcAgePolicy, warningContext string) (bool, error) {
	unlock := m.acquireChainLock(candidate.ChainID)
	defer unlock()

	chain, err := m.store.getChain(candidate.ChainID)
	if err != nil {
		return false, fmt.Errorf("failed to re-read chain metadata %s %s: %w", candidate.ChainID, warningContext, err)
	}
	if chain == nil || chain.RefCount != 0 || chain.Path != candidate.Path {
		return false, nil
	}
	if agePolicy == gcRequireExpiry {
		if chain.RefZeroAtUnix == 0 || now.Sub(time.Unix(chain.RefZeroAtUnix, 0)) < ttl {
			return false, nil
		}
	}

	if chain.Path != "" {
		_ = os.RemoveAll(filepath.Dir(chain.Path))
	}
	if err := m.store.deleteChain(candidate.ChainID); err != nil {
		logrus.Warnf("failed to delete chain metadata %s %s: %v", candidate.ChainID, warningContext, err)
		return false, nil
	}
	return true, nil
}

func pathExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func dirSizeBytes(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		size += info.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return size, nil
}

func isNotMountedError(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno == syscall.EINVAL || errno == syscall.ENOENT
	}
	var pathErr *os.PathError
	if errors.As(err, &pathErr) {
		if errors.As(pathErr.Err, &errno) {
			return errno == syscall.EINVAL || errno == syscall.ENOENT
		}
	}
	if strings.Contains(strings.ToLower(err.Error()), "not mounted") {
		return true
	}
	return false
}
