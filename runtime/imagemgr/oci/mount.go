package oci

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/sirupsen/logrus"
)

// MountImageWithContext pulls and extracts OCI layers, then mounts a readonly
// overlay rootfs.
func (m *Manager) MountImageWithContext(ctx context.Context, imageURL string) (*MountResult, error) {
	return m.MountImageWithContextAndAuth(ctx, imageURL, "")
}

func (m *Manager) MountImageWithContextAndAuth(ctx context.Context, imageURL, dockerConfigJSON string) (result *MountResult, retErr error) {
	return m.MountImageWithContextAndAuthKey(ctx, imageURL, dockerConfigJSON, imageURL)
}

func (m *Manager) MountImageWithContextAndAuthKey(ctx context.Context, imageURL, dockerConfigJSON, cacheKey string) (result *MountResult, retErr error) {
	timing, ctx := StartOCITimedOperation(ctx, "oci.MountImage", imageURL)
	defer timing.End()
	opStart := time.Now()
	if cacheKey == "" {
		cacheKey = imageURL
	}
	logrus.Infof("OCI mount start: image=%s cache_key=%s", imageURL, cacheKey)
	defer func() {
		if retErr != nil {
			logrus.Warnf("OCI mount failed: image=%s cache_key=%s cost=%s err=%v", imageURL, cacheKey, time.Since(opStart), retErr)
		}
	}()

	stageStart := time.Now()
	if imageURL == "" {
		err := fmt.Errorf("imageURL is required")
		timing.RecordError(err)
		return nil, err
	}
	timing.Stage("validate_request", time.Since(stageStart))

	unlockImage := m.acquireImageLock(imageURL)
	defer unlockImage()
	if err := m.validateRequestedImportedGeneration(imageURL, cacheKey); err != nil {
		timing.RecordError(err)
		return nil, err
	}

	var (
		txn               *OciMountTxnRecord
		mountPath         string
		envVars           []string
		reservedDigests   []string
		reservedChainIDs  []string
		rollbackResources = true
	)
	defer func() {
		if rollbackResources {
			m.rollbackMountTransaction(txn, reservedDigests, reservedChainIDs)
		}
	}()

	stageStart = time.Now()
	if info := m.getContainer(cacheKey); info != nil {
		if info.ImageConfig == nil {
			info.ImageConfig = m.resolveImageConfig(ctx, imageURL, dockerConfigJSON)
		}
		timing.Stage("reuse_in_memory_mount", time.Since(stageStart))
		logrus.Infof("OCI mount reuse in-memory: image=%s cache_key=%s mount_path=%s cost=%s", imageURL, cacheKey, info.MountPath, time.Since(opStart))
		return &MountResult{MountPath: info.MountPath, Env: info.Env, ImageConfig: cloneImageConfig(info.ImageConfig), MountID: info.MountID, LowerDirs: append([]string(nil), info.LowerDirs...)}, nil
	}

	// Reuse mounted state restored from BoltDB after restart.
	if rec, err := m.store.getMount(cacheKey); err == nil && rec != nil {
		if rec.ImageConfig == nil {
			rec.ImageConfig = m.resolveImageConfig(ctx, imageURL, dockerConfigJSON)
			if rec.ImageConfig != nil {
				if err := m.store.putMount(rec); err != nil {
					logrus.Warnf("failed to persist image config for reused mount %s: %v", imageURL, err)
				}
			}
		}
		info := &ContainerInfo{
			MountID:      rec.MountID,
			ImageURL:     rec.ImageURL,
			MountPath:    rec.MountPath,
			LayerDigests: append([]string(nil), rec.LayerDigests...),
			ChainIDs:     append([]string(nil), rec.ChainIDs...),
			LowerDirs:    append([]string(nil), rec.LowerDirs...),
			Env:          append([]string(nil), rec.Env...),
			ImageConfig:  cloneImageConfig(rec.ImageConfig),
		}
		m.setContainer(cacheKey, info)
		timing.Stage("reuse_persisted_mount", time.Since(stageStart))
		logrus.Infof("OCI mount reuse persisted: image=%s cache_key=%s mount_path=%s cost=%s", imageURL, cacheKey, rec.MountPath, time.Since(opStart))
		return &MountResult{MountPath: rec.MountPath, Env: rec.Env, ImageConfig: cloneImageConfig(rec.ImageConfig), MountID: rec.MountID, LowerDirs: append([]string(nil), rec.LowerDirs...)}, nil
	}
	timing.Stage("check_existing_mount", time.Since(stageStart))

	stageStart = time.Now()
	mountID := generateContainerID(cacheKey)
	timing.Stage("prepare_mount_id", time.Since(stageStart))

	stageStart = time.Now()
	img, err := m.fetchImage(ctx, imageURL, dockerConfigJSON)
	if err != nil {
		timing.RecordError(err)
		return nil, err
	}
	timing.Stage("fetch_image", time.Since(stageStart))

	var imageConfig *ImageConfig
	if cfg, cfgErr := img.ConfigFile(); cfgErr == nil && cfg != nil {
		envVars = cfg.Config.Env
		imageConfig = imageConfigFromContainerConfig(cfg)
		if len(envVars) == 0 {
			logrus.Debugf("image config has no env vars: %s", imageURL)
		}
	} else if cfgErr != nil {
		logrus.Warnf("failed to read image config for default process extraction: %s: %v", imageURL, cfgErr)
	}

	stageStart = time.Now()
	layers, err := img.Layers()
	if err != nil {
		err = fmt.Errorf("failed to get image layers for %s: %w", imageURL, err)
		timing.RecordError(err)
		return nil, err
	}
	if len(layers) == 0 {
		err = fmt.Errorf("image %s has no layers", imageURL)
		timing.RecordError(err)
		return nil, err
	}
	timing.Stage("list_layers", time.Since(stageStart))
	logrus.Infof("OCI mount fetched manifest: image=%s layer_count=%d", imageURL, len(layers))

	stageStart = time.Now()
	diffIDs, err := resolveImageLayerDiffIDs(img, layers)
	if err != nil {
		timing.RecordError(err)
		return nil, err
	}
	chainIDs, err := buildChainIDs(diffIDs)
	if err != nil {
		timing.RecordError(err)
		return nil, err
	}
	timing.Stage("build_chain_ids", time.Since(stageStart))

	stageStart = time.Now()
	layerDigests, layerPaths, err := m.extractLayersWithWorkers(ctx, layers)
	if err != nil {
		timing.RecordError(err)
		return nil, err
	}
	timing.Stage("extract_layers", time.Since(stageStart))
	reservedDigests = append(reservedDigests, layerDigests...)

	stageStart = time.Now()
	chainPaths, err := m.ensureChainLowerDirs(chainIDs, layerPaths)
	if err != nil {
		timing.RecordError(err)
		return nil, err
	}
	timing.Stage("prepare_lowerdirs", time.Since(stageStart))
	reservedChainIDs = append(reservedChainIDs, chainIDs...)

	lowerDirs := m.buildMountLowerDirs(reverseCopy(chainPaths))
	mountPath = filepath.Join(m.mountsDir, mountID, "merged")
	txn = &OciMountTxnRecord{
		CacheKey:      cacheKey,
		ImageURL:      imageURL,
		MountID:       mountID,
		MountPath:     mountPath,
		LayerDigests:  append([]string(nil), layerDigests...),
		ChainIDs:      append([]string(nil), chainIDs...),
		LowerDirs:     append([]string(nil), lowerDirs...),
		CreatedAtUnix: m.now().Unix(),
	}
	stageStart = time.Now()
	if err := m.store.putMountTxn(txn); err != nil {
		err = fmt.Errorf("failed to persist mount transaction for %s: %w", imageURL, err)
		timing.RecordError(err)
		return nil, err
	}
	timing.Stage("persist_mount_txn", time.Since(stageStart))

	stageStart = time.Now()
	if err := os.MkdirAll(mountPath, 0755); err != nil {
		err = fmt.Errorf("failed to create mount path %s: %w", mountPath, err)
		timing.RecordError(err)
		return nil, err
	}

	if err := m.mountFn(mountPath, lowerDirs); err != nil {
		err = fmt.Errorf("failed to mount overlay rootfs for %s: %w", imageURL, err)
		timing.RecordError(err)
		return nil, err
	}
	timing.Stage("overlay_mount", time.Since(stageStart))

	record := &OciMountRecord{
		CacheKey:      cacheKey,
		ImageURL:      imageURL,
		MountID:       mountID,
		MountPath:     mountPath,
		LayerDigests:  append([]string(nil), layerDigests...),
		ChainIDs:      append([]string(nil), chainIDs...),
		LowerDirs:     append([]string(nil), lowerDirs...),
		Env:           append([]string(nil), envVars...),
		ImageConfig:   cloneImageConfig(imageConfig),
		CreatedAtUnix: m.now().Unix(),
	}
	stageStart = time.Now()
	if err := m.store.putMount(record); err != nil {
		err = fmt.Errorf("failed to persist oci mount metadata: %w", err)
		timing.RecordError(err)
		return nil, err
	}
	if err := m.store.deleteMountTxn(cacheKey); err != nil {
		logrus.Warnf("failed to finalize mount transaction for %s: %v", imageURL, err)
	}
	timing.Stage("persist_mount_record", time.Since(stageStart))

	m.setContainer(cacheKey, &ContainerInfo{
		MountID:      mountID,
		ImageURL:     imageURL,
		MountPath:    mountPath,
		LayerDigests: append([]string(nil), layerDigests...),
		ChainIDs:     append([]string(nil), chainIDs...),
		LowerDirs:    append([]string(nil), lowerDirs...),
		Env:          append([]string(nil), envVars...),
		ImageConfig:  cloneImageConfig(imageConfig),
	})
	rollbackResources = false
	logrus.Infof("OCI mount success: image=%s cache_key=%s mount_id=%s mount_path=%s layers=%d cost=%s", imageURL, cacheKey, mountID, mountPath, len(layerDigests), time.Since(opStart))

	return &MountResult{MountPath: mountPath, Env: envVars, ImageConfig: cloneImageConfig(imageConfig), MountID: mountID, LowerDirs: append([]string(nil), lowerDirs...)}, nil
}

func (m *Manager) validateRequestedImportedGeneration(imageURL, cacheKey string) error {
	if imageURL == "" || cacheKey == "" {
		return nil
	}
	wantDigest, ok := strings.CutPrefix(cacheKey, imageURL+"@")
	if !ok || !strings.HasPrefix(wantDigest, "sha256:") {
		return nil
	}
	rec, err := m.store.getImport(imageURL)
	if err != nil {
		return fmt.Errorf("query imported image %s: %w", imageURL, err)
	}
	if rec == nil {
		return fmt.Errorf("requested imported image generation %s, but %s is not imported", cacheKey, imageURL)
	}
	if rec.ArchiveDigest != wantDigest {
		return fmt.Errorf("requested stale imported image generation %s; current generation is %s@%s", cacheKey, imageURL, rec.ArchiveDigest)
	}
	return nil
}

func (m *Manager) resolveImageConfig(ctx context.Context, imageURL, dockerConfigJSON string) *ImageConfig {
	img, err := m.fetchImage(ctx, imageURL, dockerConfigJSON)
	if err != nil {
		logrus.Warnf("failed to fetch image for config hydration: %s: %v", imageURL, err)
		return nil
	}
	cfg, err := img.ConfigFile()
	if err != nil {
		logrus.Warnf("failed to read image config for hydration: %s: %v", imageURL, err)
		return nil
	}
	return imageConfigFromContainerConfig(cfg)
}

func imageConfigFromContainerConfig(cfg *v1.ConfigFile) *ImageConfig {
	if cfg == nil {
		return nil
	}
	return &ImageConfig{
		Entrypoint: append([]string(nil), cfg.Config.Entrypoint...),
		Cmd:        append([]string(nil), cfg.Config.Cmd...),
		WorkingDir: cfg.Config.WorkingDir,
		User:       cfg.Config.User,
	}
}

func cloneImageConfig(in *ImageConfig) *ImageConfig {
	if in == nil {
		return nil
	}
	return &ImageConfig{
		Entrypoint: append([]string(nil), in.Entrypoint...),
		Cmd:        append([]string(nil), in.Cmd...),
		WorkingDir: in.WorkingDir,
		User:       in.User,
	}
}
