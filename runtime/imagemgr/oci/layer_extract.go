package oci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/sirupsen/logrus"
)

type layerExtractJob struct {
	index    int
	layer    v1.Layer
	resultCh chan<- layerExtractResult
}

type layerExtractResult struct {
	index int
	rec   *LayerRecord
	err   error
}

func (m *Manager) fetchImage(ctx context.Context, imageURL, dockerConfigJSON string) (v1.Image, error) {
	if img, ok, err := m.importedImage(ctx, imageURL); err != nil {
		return nil, err
	} else if ok {
		logrus.Infof("using imported OCI image archive for %s", imageURL)
		return img, nil
	}

	var (
		img v1.Image
		err error
	)
	if dockerConfigJSON != "" {
		img, err = m.registry.FetchImageWithFallbackWithDockerConfigJSON(ctx, imageURL, m.proxy, dockerConfigJSON)
	} else {
		img, err = m.registry.FetchImageWithFallback(ctx, imageURL, m.proxy)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to fetch OCI image %s: %w", imageURL, err)
	}
	return img, nil
}

func (m *Manager) extractLayersWithWorkers(ctx context.Context, layers []v1.Layer) ([]string, []string, error) {
	if len(layers) == 0 {
		return nil, nil, nil
	}
	m.ensureLayerWorkerPool()
	batchStart := time.Now()
	logrus.Infof("OCI layer extraction start: layers=%d workers=%d", len(layers), m.layerWorkers)

	results := make(chan layerExtractResult, len(layers))
	submitted := 0
	var submitErr error
submitLoop:
	for i, layer := range layers {
		select {
		case <-ctx.Done():
			submitErr = fmt.Errorf("layer extraction canceled: %w", ctx.Err())
			logrus.Warnf("OCI layer extraction canceled before submit: submitted=%d/%d cost=%s err=%v", submitted, len(layers), time.Since(batchStart), submitErr)
			break submitLoop
		case <-m.stopCh:
			err := fmt.Errorf("oci manager is closing")
			logrus.Warnf("OCI layer extraction interrupted before submit: submitted=%d/%d cost=%s err=%v", submitted, len(layers), time.Since(batchStart), err)
			return nil, nil, err
		case m.layerJobs <- layerExtractJob{
			index:    i,
			layer:    layer,
			resultCh: results,
		}:
			submitted++
		}
	}

	layerDigests := make([]string, len(layers))
	layerPaths := make([]string, len(layers))
	firstErr := submitErr
	success := 0
	for i := 0; i < submitted; i++ {
		var res layerExtractResult
		select {
		case <-m.stopCh:
			err := fmt.Errorf("oci manager is closing")
			logrus.Warnf("OCI layer extraction interrupted while waiting results: received=%d/%d cost=%s err=%v", i, submitted, time.Since(batchStart), err)
			return nil, nil, err
		case res = <-results:
		}
		if ctx.Err() != nil && firstErr == nil {
			firstErr = fmt.Errorf("layer extraction canceled: %w", ctx.Err())
		}
		if res.err != nil {
			if firstErr == nil {
				firstErr = res.err
			}
			continue
		}
		layerDigests[res.index] = res.rec.Digest
		layerPaths[res.index] = res.rec.Path
		success++
	}
	if firstErr != nil {
		m.rollbackReservedLayerRefs(layerDigests)
		logrus.Warnf("OCI layer extraction failed: submitted=%d success=%d cost=%s err=%v", submitted, success, time.Since(batchStart), firstErr)
		return nil, nil, firstErr
	}
	logrus.Infof("OCI layer extraction finished: submitted=%d success=%d cost=%s", submitted, success, time.Since(batchStart))
	return layerDigests, layerPaths, nil
}

func (m *Manager) ensureLayerWorkerPool() {
	m.layerPoolMu.Lock()
	defer m.layerPoolMu.Unlock()

	m.layerPoolOnce.Do(func() {
		workers := m.layerWorkers
		if workers <= 0 {
			workers = 1
		}
		m.layerWorkers = workers

		queueSize := workers * 4
		if queueSize < workers {
			queueSize = workers
		}
		m.layerJobs = make(chan layerExtractJob, queueSize)

		for i := 0; i < workers; i++ {
			m.layerPoolWG.Add(1)
			go func() {
				defer m.layerPoolWG.Done()
				for {
					select {
					case <-m.stopCh:
						return
					case job := <-m.layerJobs:
						rec, err := m.ensureLayerExtracted(job.layer)
						job.resultCh <- layerExtractResult{
							index: job.index,
							rec:   rec,
							err:   err,
						}
					}
				}
			}()
		}
	})
}

func (m *Manager) ensureLayerExtracted(layer v1.Layer) (*LayerRecord, error) {
	start := time.Now()
	digest, err := layer.Digest()
	if err != nil {
		return nil, fmt.Errorf("failed to get layer digest: %w", err)
	}
	digestStr := digest.String()
	unlockLayer := m.acquireLayerLock(digestStr)
	defer unlockLayer()

	rec, err := m.store.getLayer(digestStr)
	if err != nil {
		return nil, fmt.Errorf("failed to read layer metadata %s: %w", digestStr, err)
	}
	layerDir, err := m.store.getOrCreateLayerDir(digestStr)
	if err != nil {
		return nil, fmt.Errorf("failed to allocate layer dir for %s: %w", digestStr, err)
	}
	layerRoot := filepath.Join(m.layersDir, layerDir)
	layerPath := filepath.Join(layerRoot, "fs")

	if rec != nil && rec.Path != "" && pathExists(rec.Path) {
		rec, err = m.store.incrementLayerRef(digestStr, m.now().Unix())
		if err != nil {
			return nil, fmt.Errorf("failed to reserve layer metadata %s: %w", digestStr, err)
		}
		logrus.Infof("OCI layer cache hit: digest=%s path=%s ref_count=%d cost=%s", digestStr, rec.Path, rec.RefCount, time.Since(start))
		return rec, nil
	}

	// Recovery for crash window: extracted layer exists but metadata was not persisted.
	if pathExists(layerPath) {
		size, err := dirSizeBytes(layerPath)
		if err != nil {
			return nil, fmt.Errorf("failed to stat recovered layer path %s: %w", layerPath, err)
		}
		recoveredRefCount := 1
		if rec != nil {
			recoveredRefCount = rec.RefCount + 1
		}
		rec = &LayerRecord{
			Digest:        digestStr,
			Path:          layerPath,
			SizeBytes:     size,
			RefCount:      recoveredRefCount,
			RefZeroAtUnix: 0,
			LastUsedUnix:  m.now().Unix(),
		}
		if err := m.store.putLayer(rec); err != nil {
			return nil, fmt.Errorf("failed to recover layer metadata %s: %w", digestStr, err)
		}
		logrus.Infof("OCI layer cache recovered: digest=%s path=%s size_bytes=%d cost=%s", digestStr, layerPath, size, time.Since(start))
		return rec, nil
	}
	logrus.Infof("OCI layer cache miss: digest=%s action=download_extract", digestStr)

	tmpDir := filepath.Join(layerRoot, fmt.Sprintf("tmp-%d", m.now().UnixNano()))
	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create temp layer dir %s: %w", tmpDir, err)
	}
	defer os.RemoveAll(tmpDir)

	rc, err := layer.Uncompressed()
	if err != nil {
		logrus.Warnf("OCI layer download failed: digest=%s cost=%s err=%v", digestStr, time.Since(start), err)
		return nil, fmt.Errorf("failed to open uncompressed layer %s: %w", digestStr, err)
	}
	defer rc.Close()

	size, err := extractLayerTar(rc, tmpDir)
	if err != nil {
		logrus.Warnf("OCI layer extract failed: digest=%s tmp_dir=%s cost=%s err=%v", digestStr, tmpDir, time.Since(start), err)
		return nil, fmt.Errorf("failed to extract layer %s: %w", digestStr, err)
	}

	if err := os.MkdirAll(layerRoot, 0755); err != nil {
		return nil, fmt.Errorf("failed to create layer root %s: %w", layerRoot, err)
	}
	if err := os.RemoveAll(layerPath); err != nil {
		return nil, fmt.Errorf("failed to cleanup old layer path %s: %w", layerPath, err)
	}
	if err := os.Rename(tmpDir, layerPath); err != nil {
		return nil, fmt.Errorf("failed to place extracted layer %s: %w", digestStr, err)
	}

	rec = &LayerRecord{
		Digest:        digestStr,
		Path:          layerPath,
		SizeBytes:     size,
		RefCount:      1,
		RefZeroAtUnix: 0,
		LastUsedUnix:  m.now().Unix(),
	}
	if err := m.store.putLayer(rec); err != nil {
		return nil, fmt.Errorf("failed to persist layer metadata %s: %w", digestStr, err)
	}
	logrus.Infof("OCI layer download/extract success: digest=%s path=%s size_bytes=%d cost=%s", digestStr, layerPath, size, time.Since(start))
	return rec, nil
}

func resolveImageLayerDiffIDs(img v1.Image, layers []v1.Layer) ([]v1.Hash, error) {
	cfg, err := img.ConfigFile()
	if err == nil {
		if len(cfg.RootFS.DiffIDs) != len(layers) {
			return nil, fmt.Errorf("image config diff_ids length %d does not match layer count %d", len(cfg.RootFS.DiffIDs), len(layers))
		}
		diffIDs := make([]v1.Hash, len(cfg.RootFS.DiffIDs))
		copy(diffIDs, cfg.RootFS.DiffIDs)
		return diffIDs, nil
	}

	diffIDs := make([]v1.Hash, len(layers))
	for i, layer := range layers {
		diffID, diffErr := layer.DiffID()
		if diffErr != nil {
			return nil, fmt.Errorf("failed to resolve diffID for layer %d: %w", i, diffErr)
		}
		diffIDs[i] = diffID
	}
	return diffIDs, nil
}

func buildChainIDs(diffIDs []v1.Hash) ([]string, error) {
	if len(diffIDs) == 0 {
		return nil, fmt.Errorf("diffIDs is empty")
	}

	chainIDs := make([]string, len(diffIDs))
	parent := ""
	for i, diffID := range diffIDs {
		if diffID.String() == "" {
			return nil, fmt.Errorf("diffID at index %d is empty", i)
		}
		if parent == "" {
			parent = diffID.String()
		} else {
			sum := sha256.Sum256([]byte(parent + " " + diffID.String()))
			parent = "sha256:" + hex.EncodeToString(sum[:])
		}
		chainIDs[i] = parent
	}
	return chainIDs, nil
}
