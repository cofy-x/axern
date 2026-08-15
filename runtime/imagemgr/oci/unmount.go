package oci

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"
)

var ErrMountNotFound = errors.New("OCI mount not found")

// UnmountImageWithContext unmounts an OCI overlay mount and updates layer references.
func (m *Manager) UnmountImageWithContext(ctx context.Context, imageURL string) (retErr error) {
	cacheKey := imageURL
	if resolved, imported, err := m.ResolveImportedImageCacheKey(imageURL); err != nil {
		return err
	} else if imported {
		cacheKey = resolved
	}
	return m.UnmountImageWithContextAndKey(ctx, imageURL, cacheKey)
}

func (m *Manager) UnmountImageWithContextAndKey(ctx context.Context, imageURL, cacheKey string) (retErr error) {
	timing, _ := StartOCITimedOperation(ctx, "oci.UnmountImage", imageURL)
	defer timing.End()
	opStart := time.Now()
	if cacheKey == "" {
		cacheKey = imageURL
	}
	logrus.Infof("OCI unmount start: image=%s cache_key=%s", imageURL, cacheKey)
	defer func() {
		if retErr != nil {
			logrus.Warnf("OCI unmount failed: image=%s cache_key=%s cost=%s err=%v", imageURL, cacheKey, time.Since(opStart), retErr)
		}
	}()

	stageStart := time.Now()
	if imageURL == "" {
		err := fmt.Errorf("imageURL is required")
		timing.RecordError(err)
		return err
	}
	timing.Stage("validate_request", time.Since(stageStart))

	unlockImage := m.acquireImageLock(imageURL)
	defer unlockImage()

	return m.unmountImageLocked(timing, imageURL, cacheKey, opStart)
}

func (m *Manager) unmountImageLocked(timing *OCITimedOperation, imageURL, cacheKey string, opStart time.Time) error {
	if opStart.IsZero() {
		opStart = time.Now()
	}
	stageStart := time.Now()
	info := m.getContainer(cacheKey)
	if info == nil {
		rec, err := m.store.getMount(cacheKey)
		if err != nil {
			err = fmt.Errorf("failed to query mount metadata for %s (%s): %w", imageURL, cacheKey, err)
			if timing != nil {
				timing.RecordError(err)
			}
			return err
		}
		if rec == nil {
			err = fmt.Errorf("%w: image %s", ErrMountNotFound, imageURL)
			if timing != nil {
				timing.RecordError(err)
			}
			return err
		}
		info = &ContainerInfo{
			MountID:      rec.MountID,
			ImageURL:     rec.ImageURL,
			MountPath:    rec.MountPath,
			LayerDigests: append([]string(nil), rec.LayerDigests...),
			ChainIDs:     append([]string(nil), rec.ChainIDs...),
			LowerDirs:    append([]string(nil), rec.LowerDirs...),
		}
	}
	if timing != nil {
		timing.Stage("lookup_mount", time.Since(stageStart))
	}

	stageStart = time.Now()
	if err := m.unmountFn(info.MountPath); err != nil && !isNotMountedError(err) {
		err = fmt.Errorf("failed to unmount overlay at %s: %w", info.MountPath, err)
		if timing != nil {
			timing.RecordError(err)
		}
		return err
	}
	if timing != nil {
		timing.Stage("overlay_unmount", time.Since(stageStart))
	}

	stageStart = time.Now()
	if err := m.store.deleteMount(cacheKey); err != nil {
		err = fmt.Errorf("failed to delete mount metadata for %s (%s): %w", imageURL, cacheKey, err)
		if timing != nil {
			timing.RecordError(err)
		}
		return err
	}
	if timing != nil {
		timing.Stage("delete_mount_record", time.Since(stageStart))
	}

	stageStart = time.Now()
	for _, digest := range info.LayerDigests {
		if _, err := m.store.decrementLayerRef(digest, m.now().Unix()); err != nil && !errors.Is(err, ErrLayerNotFound) {
			logrus.Warnf("failed to update layer refcount for %s: %v", digest, err)
		}
	}
	if timing != nil {
		timing.Stage("decrement_layer_refs", time.Since(stageStart))
	}

	stageStart = time.Now()
	for _, chainID := range info.ChainIDs {
		if _, err := m.store.decrementChainRef(chainID, m.now().Unix()); err != nil && !errors.Is(err, ErrChainNotFound) {
			logrus.Warnf("failed to update chain refcount for %s: %v", chainID, err)
		}
	}
	if timing != nil {
		timing.Stage("decrement_chain_refs", time.Since(stageStart))
	}

	m.deleteContainer(cacheKey)
	_ = os.RemoveAll(filepath.Dir(info.MountPath))
	if err := m.pruneImportedGenerations(); err != nil {
		logrus.WithError(err).Warn("prune unreferenced imported generations after unmount")
	}
	logrus.Infof("OCI unmount success: image=%s cache_key=%s mount_path=%s layers=%d cost=%s", imageURL, cacheKey, info.MountPath, len(info.LayerDigests), time.Since(opStart))

	return nil
}
