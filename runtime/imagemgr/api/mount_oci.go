package api

import (
	"context"
	"errors"
	"fmt"
	"io"

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
	if _, err := w.ociMgr.HasImportedContent(req.CacheKey); err != nil {
		return nil, fmt.Errorf("query imported content: %w", err)
	}
	if req.CacheKey == "" {
		key, imported, err := w.ociMgr.ResolveImportedImageCacheKey(req.ImageURL)
		if err != nil {
			return nil, fmt.Errorf("query imported image: %w", err)
		}
		if imported {
			copy := *req
			copy.CacheKey = key
			req = &copy
		}
	}
	key := mountKey(req.ImageURL, req.CacheKey)
	unlock := w.lockMount(key)
	defer unlock()
	if lease, record, err := w.existingLease(req.LeaseID); err != nil {
		return nil, err
	} else if record != nil {
		if lease.MountKey != key {
			return nil, fmt.Errorf("lease %q already owns mount %q", req.LeaseID, lease.MountKey)
		}
		return w.acquireRecordedOCI(ctx, req, record)
	}
	if record, err := w.mountStore.GetMount(key); err != nil {
		return nil, err
	} else if record != nil {
		return w.acquireRecordedOCI(ctx, req, record)
	}
	response, record, err := w.ensureOCIOverlayMounted(ctx, req)
	if err != nil {
		return nil, err
	}
	if _, err := w.mountStore.Acquire(record, req.LeaseID, req.Owner); err != nil {
		return nil, errors.Join(fmt.Errorf("persist mount lease: %w", err), w.unmountResource(ctx, record))
	}
	return response, nil
}

func (w *HttpWorker) ensureOCIOverlayMounted(ctx context.Context, req *OCIMountRequest) (*OCIMountResponse, *mountstore.Record, error) {
	result, err := w.ociMgr.MountImageWithContextAndAuthKey(ctx, req.ImageURL, req.DockerConfigJSON, mountKey(req.ImageURL, req.CacheKey))
	if err != nil {
		return nil, nil, err
	}
	descriptor, err := immutableMountDescriptor(result.MountPath, req.LeaseID, "overlay", result.MountID, result.LowerDirs, nil)
	if err != nil {
		cause := fmt.Errorf("describe OCI immutable mount: %w", err)
		return nil, nil, errors.Join(cause, w.ociMgr.UnmountImageWithContextAndKey(ctx, req.ImageURL, mountKey(req.ImageURL, req.CacheKey)))
	}
	return &OCIMountResponse{
		MountPath:      result.MountPath,
		Env:            result.Env,
		ImageConfig:    imageConfigFromOCI(result.ImageConfig),
		ImmutableMount: descriptor,
	}, newOCIMountRecord(req, result.MountPath), nil
}

func (w *HttpWorker) acquireRecordedOCI(ctx context.Context, req *OCIMountRequest, record *mountstore.Record) (*OCIMountResponse, error) {
	if err := w.validateMountType(record); err != nil {
		return nil, err
	}
	result, err := w.ociMgr.MountImageWithContextAndAuthKey(ctx, req.ImageURL, req.DockerConfigJSON, record.CacheKey)
	if err != nil {
		return nil, err
	}
	descriptor, err := immutableMountDescriptor(result.MountPath, req.LeaseID, "overlay", result.MountID, result.LowerDirs, nil)
	if err != nil {
		return nil, err
	}
	if _, err := w.mountStore.Acquire(record, req.LeaseID, req.Owner); err != nil {
		return nil, fmt.Errorf("persist mount lease: %w", err)
	}
	return &OCIMountResponse{MountPath: result.MountPath, Env: result.Env, ImageConfig: imageConfigFromOCI(result.ImageConfig), ImmutableMount: descriptor}, nil
}

func (w *HttpWorker) validateMountType(record *mountstore.Record) error {
	if record.MountType != string(MountTypeOCI) || record.NydusImageURL != "" {
		return fmt.Errorf("retired or unsupported persisted image format %q; only OCI resources are accepted", record.MountType)
	}
	return nil
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

func (w *HttpWorker) UnmountOCI(ctx context.Context, req *OCIUmountRequest) error {
	if w.ociMgr == nil {
		return fmt.Errorf("oci manager is not initialized")
	}
	return w.releaseLease(req.LeaseID, func(record *mountstore.Record) error { return w.unmountResource(ctx, record) })
}

func (w *HttpWorker) unmountResource(ctx context.Context, record *mountstore.Record) error {
	if err := w.validateMountType(record); err != nil {
		return err
	}
	err := w.ociMgr.UnmountImageWithContextAndKey(ctx, record.ImageURL, record.CacheKey)
	if errors.Is(err, oci.ErrMountNotFound) {
		return nil
	}
	return err
}

func (w *HttpWorker) ImportOCI(ctx context.Context, imageRef string, archive io.Reader) (*OCIImportResponse, error) {
	if w.ociMgr == nil {
		return nil, fmt.Errorf("oci manager is not initialized")
	}
	if imageRef == "" {
		return nil, fmt.Errorf("image_ref is required")
	}
	result, err := w.ociMgr.ImportImage(ctx, imageRef, archive)
	if err != nil {
		return nil, err
	}
	immutableRef, err := oci.ImmutableImageRef(result.ImageURL, result.ContentDigest)
	if err != nil {
		return nil, err
	}
	return &OCIImportResponse{
		SourceRef: result.SourceRef, CanonicalRef: result.ImageURL, ImmutableRef: immutableRef,
		ContentDigest: result.ContentDigest, ArchiveDigest: result.ArchiveDigest,
		Platform:  formatImagePlatform(result.PlatformOS, result.PlatformArch, result.PlatformVariant),
		SizeBytes: result.SizeBytes, Reused: result.Reused,
	}, nil
}

func (w *HttpWorker) ResolveOCI(imageRef string) (*OCIResolveResponse, error) {
	if w.ociMgr == nil {
		return nil, fmt.Errorf("oci manager is not initialized")
	}
	canonicalRef, cacheKey, imported, err := w.ociMgr.ResolveImageCacheKey(imageRef)
	if err != nil {
		return nil, err
	}
	return &OCIResolveResponse{CanonicalRef: canonicalRef, CacheKey: cacheKey, Imported: imported}, nil
}

func formatImagePlatform(osName, arch, variant string) string {
	platform := osName + "/" + arch
	if variant != "" {
		platform += "/" + variant
	}
	return platform
}

func (w *HttpWorker) ListMountedOCIImages() ([]string, error) {
	if w.mountStore != nil {
		records, err := w.mountStore.List()
		if err != nil {
			return nil, fmt.Errorf("failed to list mount records: %w", err)
		}
		imageURLs := make([]string, 0, len(records))
		for _, record := range records {
			if err := w.validateMountType(&record); err != nil {
				return nil, err
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
			if err := w.validateMountType(&record); err != nil {
				return nil, err
			}
			leaseCount, err := w.mountStore.LeaseCount(record.CacheKey)
			if err != nil {
				return nil, fmt.Errorf("count leases for mount %s: %w", record.CacheKey, err)
			}
			mounts = append(mounts, MountedImageDetail{
				ImageURL:   record.ImageURL,
				CacheKey:   record.CacheKey,
				MountType:  MountType(record.MountType),
				MountPath:  record.MountPoint,
				LeaseCount: leaseCount,
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
	response := &InventoryResponse{MountedImages: mounts}
	if w.ociMgr != nil {
		imports, err := w.ociMgr.ListImportedImages()
		if err != nil {
			return nil, err
		}
		response.ImportedImages = make([]ImportedImageDetail, 0, len(imports))
		for _, record := range imports {
			response.ImportedImages = append(response.ImportedImages, ImportedImageDetail{CacheKey: record.CacheKey, ImageRef: record.ImageURL, ContentDigest: record.ContentDigest, ArchiveDigest: record.ArchiveDigest, Platform: formatImagePlatform(record.PlatformOS, record.PlatformArch, record.PlatformVariant), SizeBytes: record.SizeBytes, ImportedAtUnix: record.ImportedAtUnix})
		}
	}
	return response, nil
}

func mountKey(imageURL, cacheKey string) string {
	if cacheKey != "" {
		return cacheKey
	}
	return imageURL
}
