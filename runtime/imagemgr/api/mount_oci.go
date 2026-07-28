package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/sirupsen/logrus"

	"github.com/cofy-x/axern/runtime/imagemgr/imagefsd"
	"github.com/cofy-x/axern/runtime/imagemgr/internal/mountstore"
	"github.com/cofy-x/axern/runtime/imagemgr/oci"
)

func (w *HttpWorker) MountOCI(ctx context.Context, req *OCIMountRequest) (*OCIMountResponse, error) {
	if w.ociMgr == nil {
		return nil, fmt.Errorf("oci manager is not initialized")
	}
	if req.ImageURL == "" {
		return nil, fmt.Errorf("image_url is required")
	}
	if err := validateLeaseID(req.LeaseID); err != nil {
		return nil, err
	}
	if err := validateOwner(req.Owner); err != nil {
		return nil, err
	}
	if w.mountStore == nil {
		return nil, fmt.Errorf("mount store is not initialized")
	}

	importedCacheKey, imported, err := w.ociMgr.ResolveImportedImageCacheKey(req.ImageURL)
	if err != nil {
		return nil, fmt.Errorf("query imported image %s: %w", req.ImageURL, err)
	}
	if imported {
		logrus.Infof("using imported OCI image archive for %s; skipping Nydus detection", req.ImageURL)
		importedReq := *req
		importedReq.CacheKey = importedCacheKey
		req = &importedReq
	}
	key := mountKey(req.ImageURL, req.CacheKey)
	unlock := w.lockMount(key)
	defer unlock()

	if lease, record, err := w.existingLease(req.LeaseID); err != nil {
		return nil, err
	} else if record != nil {
		matchesRequest := lease.MountKey == key
		if MountType(record.MountType) == MountTypeNydus {
			matchesRequest = record.NydusImageURL == req.ImageURL ||
				(w.nydusSuffix != "" && record.NydusImageURL == req.ImageURL+w.nydusSuffix)
		}
		if !matchesRequest {
			return nil, fmt.Errorf("lease %q already owns mount %q", req.LeaseID, lease.MountKey)
		}
		if lease.MountKey != key {
			resourceUnlock := w.lockMount(lease.MountKey)
			defer resourceUnlock()
		}
		return w.acquireRecordedOCI(ctx, req, record)
	}
	if record, err := w.mountStore.GetMount(key); err != nil {
		return nil, err
	} else if record != nil {
		return w.acquireRecordedOCI(ctx, req, record)
	}

	response, record, err := w.mountNewOCI(ctx, req, imported)
	if err != nil {
		return nil, err
	}
	if record.CacheKey != key {
		resourceUnlock := w.lockMount(record.CacheKey)
		defer resourceUnlock()
		// The Nydus route and direct Nydus API share one resource identity. A
		// final consumer may have released it while route detection ran, so
		// confirm the resource under its canonical lock before recording lease.
		return w.acquireRecordedOCI(ctx, req, record)
	}
	if _, err := w.mountStore.Acquire(record, req.LeaseID, req.Owner); err != nil {
		if rollbackErr := w.unmountResource(ctx, record); rollbackErr != nil {
			return nil, fmt.Errorf("persist mount lease: %w; rollback mount: %v", err, rollbackErr)
		}
		return nil, fmt.Errorf("persist mount lease: %w", err)
	}
	return response, nil
}

func (w *HttpWorker) mountNewOCI(ctx context.Context, req *OCIMountRequest, imported bool) (*OCIMountResponse, *mountstore.Record, error) {
	if imported {
		response, record, err := w.ensureOCIOverlayMounted(ctx, req)
		return response, record, err
	}

	if w.nydusClient != nil {
		// Try original URL
		if attempt, err := w.tryMountNydus(ctx, req.ImageURL, req.DockerConfigJSON); err == nil {
			return &OCIMountResponse{MountPath: attempt.mountPoint, Env: attempt.env}, newNydusMountRecord(req, req.ImageURL, attempt.mountPoint), nil
		} else if attempt.detected {
			logrus.WithError(err).Warnf("detected Nydus image for %s but Nydus mount failed, skip OCI fallback", req.ImageURL)
			return nil, nil, err
		}

		// Try with suffix if configured
		if w.nydusSuffix != "" {
			imageWithSuffix := req.ImageURL + w.nydusSuffix
			logrus.Infof("trying Nydus detection with suffix: %s", imageWithSuffix)
			if attempt, err := w.tryMountNydus(ctx, imageWithSuffix, req.DockerConfigJSON); err == nil {
				return &OCIMountResponse{MountPath: attempt.mountPoint, Env: attempt.env}, newNydusMountRecord(req, imageWithSuffix, attempt.mountPoint), nil
			} else if attempt.detected {
				logrus.WithError(err).Warnf("detected Nydus image for %s via suffix %s but Nydus mount failed, skip OCI fallback", req.ImageURL, imageWithSuffix)
				return nil, nil, err
			}
		}
	}

	// Fallback to regular OCI mount flow.
	logrus.Infof("no Nydus image detected, using OCI overlay mount for %s", req.ImageURL)
	return w.ensureOCIOverlayMounted(ctx, req)
}

func (w *HttpWorker) ensureOCIOverlayMounted(ctx context.Context, req *OCIMountRequest) (*OCIMountResponse, *mountstore.Record, error) {
	result, err := w.ociMgr.MountImageWithContextAndAuthKey(ctx, req.ImageURL, req.DockerConfigJSON, mountKey(req.ImageURL, req.CacheKey))
	if err != nil {
		return nil, nil, err
	}
	return &OCIMountResponse{
		MountPath:   result.MountPath,
		Env:         result.Env,
		ImageConfig: imageConfigFromOCI(result.ImageConfig),
	}, newOCIMountRecord(req, result.MountPath), nil
}

func (w *HttpWorker) acquireRecordedOCI(ctx context.Context, req *OCIMountRequest, record *mountstore.Record) (*OCIMountResponse, error) {
	var response *OCIMountResponse
	switch MountType(record.MountType) {
	case MountTypeNydus:
		info, err := w.ensureNydusMounted(ctx, &NydusMountRequest{ImageURL: record.NydusImageURL, DockerConfigJSON: req.DockerConfigJSON})
		if err != nil {
			return nil, err
		}
		response = &OCIMountResponse{MountPath: info.MountPath, Env: info.Env}
	case MountTypeOCI:
		result, err := w.ociMgr.MountImageWithContextAndAuthKey(ctx, req.ImageURL, req.DockerConfigJSON, record.CacheKey)
		if err != nil {
			return nil, err
		}
		response = &OCIMountResponse{MountPath: result.MountPath, Env: result.Env, ImageConfig: imageConfigFromOCI(result.ImageConfig)}
	default:
		return nil, fmt.Errorf("unsupported persisted mount type %q", record.MountType)
	}
	if _, err := w.mountStore.Acquire(record, req.LeaseID, req.Owner); err != nil {
		return nil, fmt.Errorf("persist mount lease: %w", err)
	}
	return response, nil
}

func imageConfigFromOCI(in *oci.ImageConfig) *ImageConfig {
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

func newOCIMountRecord(req *OCIMountRequest, mountPoint string) *mountstore.Record {
	return &mountstore.Record{
		CacheKey:   mountKey(req.ImageURL, req.CacheKey),
		ImageURL:   req.ImageURL,
		MountType:  string(MountTypeOCI),
		MountPoint: mountPoint,
	}
}

func newNydusMountRecord(_ *OCIMountRequest, nydusImageURL, mountPoint string) *mountstore.Record {
	return &mountstore.Record{
		CacheKey:      "nydus:" + nydusImageURL,
		ImageURL:      nydusImageURL,
		MountType:     string(MountTypeNydus),
		NydusImageURL: nydusImageURL,
		MountPoint:    mountPoint,
	}
}

func (w *HttpWorker) UnmountOCI(ctx context.Context, req *OCIUmountRequest) error {
	if w.ociMgr == nil {
		return fmt.Errorf("oci manager is not initialized")
	}
	return w.releaseLease(req.LeaseID, func(record *mountstore.Record) error { return w.unmountResource(ctx, record) })
}

func (w *HttpWorker) unmountResource(ctx context.Context, record *mountstore.Record) error {
	switch MountType(record.MountType) {
	case MountTypeNydus:
		return w.unmountNydusResource(ctx, record.NydusImageURL)
	case MountTypeOCI:
		err := w.ociMgr.UnmountImageWithContextAndKey(ctx, record.ImageURL, record.CacheKey)
		if errors.Is(err, oci.ErrMountNotFound) {
			return nil
		}
		return err
	case MountTypeOSS:
		return w.unmountOSSResource(ctx, record)
	default:
		return fmt.Errorf("unsupported persisted mount type %q", record.MountType)
	}
}

func (w *HttpWorker) CleanupDaemon(req *CleanupDaemonRequest) error {
	if req.DaemonID == "" {
		return fmt.Errorf("daemon_id is required")
	}
	return w.mgr.CleanupDaemon(req.DaemonID)
}

func (w *HttpWorker) ImportOCI(ctx context.Context, req *OCIImportRequest) (*OCIImportResponse, error) {
	if w.ociMgr == nil {
		return nil, fmt.Errorf("oci manager is not initialized")
	}
	if req.ImageRef == "" {
		return nil, fmt.Errorf("image_ref is required")
	}
	if req.ArchivePath == "" {
		return nil, fmt.Errorf("archive_path is required")
	}
	result, err := w.ociMgr.ImportImageArchive(ctx, req.ImageRef, req.ArchivePath)
	if err != nil {
		return nil, err
	}
	return &OCIImportResponse{
		ImageRef:       result.ImageURL,
		ArchivePath:    result.ArchivePath,
		ArchiveDigest:  result.ArchiveDigest,
		SizeBytes:      result.SizeBytes,
		ImportedAtUnix: result.ImportedAtUnix,
	}, nil
}

func (w *HttpWorker) ListDaemons() ([]imagefsd.DaemonInfo, error) {
	return w.mgr.ListDaemons(), nil
}

func (w *HttpWorker) ListMountedOCIImages() ([]string, error) {
	if w.mountStore != nil {
		records, err := w.mountStore.List()
		if err != nil {
			return nil, fmt.Errorf("failed to list mount records: %w", err)
		}
		imageURLs := make([]string, 0, len(records))
		for _, record := range records {
			if MountType(record.MountType) == MountTypeOSS {
				continue
			}
			imageURLs = append(imageURLs, record.ImageURL)
		}
		return imageURLs, nil
	}
	if w.ociMgr == nil {
		return nil, fmt.Errorf("oci manager is not initialized")
	}
	return w.ociMgr.ListMountedImageURLs()
}

func (w *HttpWorker) ListMountedOCIDetails() ([]MountedImageDetail, error) {
	if w.mountStore != nil {
		records, err := w.mountStore.List()
		if err != nil {
			return nil, fmt.Errorf("failed to list mount records: %w", err)
		}
		mounts := make([]MountedImageDetail, 0, len(records))
		for _, record := range records {
			if MountType(record.MountType) == MountTypeOSS {
				continue
			}
			leaseCount, err := w.mountStore.LeaseCount(record.CacheKey)
			if err != nil {
				return nil, fmt.Errorf("count leases for mount %s: %w", record.CacheKey, err)
			}
			mounts = append(mounts, MountedImageDetail{
				ImageURL:      record.ImageURL,
				CacheKey:      record.CacheKey,
				MountType:     MountType(record.MountType),
				NydusImageURL: record.NydusImageURL,
				MountPath:     record.MountPoint,
				LeaseCount:    leaseCount,
			})
		}
		return mounts, nil
	}
	if w.ociMgr == nil {
		return nil, fmt.Errorf("oci manager is not initialized")
	}
	records, err := w.ociMgr.ListMountedDetails()
	if err != nil {
		return nil, err
	}
	mounts := make([]MountedImageDetail, 0, len(records))
	for _, record := range records {
		mounts = append(mounts, MountedImageDetail{
			ImageURL:  record.ImageURL,
			CacheKey:  record.CacheKey,
			MountType: MountTypeOCI,
			MountPath: record.MountPath,
		})
	}
	return mounts, nil
}

func (w *HttpWorker) Inventory() (*InventoryResponse, error) {
	mounts := []MountedImageDetail{}
	var err error
	if w.mountStore != nil || w.ociMgr != nil {
		mounts, err = w.ListMountedOCIDetails()
		if err != nil {
			return nil, err
		}
	}
	daemons, err := w.ListDaemons()
	if err != nil {
		return nil, err
	}

	resp := &InventoryResponse{
		MountedImages: mounts,
		Daemons:       daemons,
	}
	if w.ociMgr != nil {
		imports, err := w.ociMgr.ListImportedImages()
		if err != nil {
			return nil, err
		}
		resp.ImportedImages = make([]ImportedImageDetail, 0, len(imports))
		for _, rec := range imports {
			resp.ImportedImages = append(resp.ImportedImages, ImportedImageDetail{
				ImageRef:       rec.ImageURL,
				ArchivePath:    rec.ArchivePath,
				ArchiveDigest:  rec.ArchiveDigest,
				SizeBytes:      rec.SizeBytes,
				ImportedAtUnix: rec.ImportedAtUnix,
			})
		}
	}
	resp.Locality, resp.LocalityError = w.buildLocalityEntries(mounts, daemons)
	chunkDB, err := w.mgr.ChunkDBStats()
	if err != nil {
		resp.ChunkDBError = err.Error()
		return resp, nil
	}
	resp.ChunkDB = chunkDB
	return resp, nil
}

func mountKey(imageURL, cacheKey string) string {
	if cacheKey != "" {
		return cacheKey
	}
	return imageURL
}
