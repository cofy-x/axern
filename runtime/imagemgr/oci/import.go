package oci

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/sirupsen/logrus"
)

// ImportResult describes a completed node-local image import.
type ImportResult struct {
	ImageURL       string
	ArchivePath    string
	ArchiveDigest  string
	SizeBytes      int64
	ImportedAtUnix int64
}

type archiveCopyResult struct {
	SizeBytes     int64
	ArchiveDigest string
}

// ImportImageArchive copies a Docker archive into the local OCI store and
// registers it under imageURL for future mounts. Each imported archive records
// a content digest so callers can address the mounted rootfs generation
// explicitly instead of reusing stale mutable tags.
func (m *Manager) ImportImageArchive(ctx context.Context, imageURL, archivePath string) (*ImportResult, error) {
	imageURL = strings.TrimSpace(imageURL)
	archivePath = strings.TrimSpace(archivePath)
	if imageURL == "" {
		return nil, fmt.Errorf("image ref is required")
	}
	if archivePath == "" {
		return nil, fmt.Errorf("archive path is required")
	}
	if _, err := os.Stat(archivePath); err != nil {
		return nil, fmt.Errorf("stat archive %s: %w", archivePath, err)
	}

	img, err := imageFromArchive(archivePath, imageURL)
	if err != nil {
		return nil, fmt.Errorf("invalid image archive for %s: %w", imageURL, err)
	}
	if layers, err := img.Layers(); err != nil {
		return nil, fmt.Errorf("invalid image archive for %s: %w", imageURL, err)
	} else if len(layers) == 0 {
		return nil, fmt.Errorf("invalid image archive for %s: image has no layers", imageURL)
	}

	unlockImage := m.acquireImageLock(imageURL)
	defer unlockImage()

	targetPath := filepath.Join(m.importsDir, importArchiveName(imageURL)+".tar")
	tmpPath := filepath.Join(m.importsDir, fmt.Sprintf(".%s.%d.tmp", importArchiveName(imageURL), m.now().UnixNano()))
	copyResult, err := copyArchiveAndComputeDigest(tmpPath, archivePath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return nil, err
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return nil, fmt.Errorf("replace imported archive %s: %w", targetPath, err)
	}

	record := &ImportedImageRecord{
		ImageURL:       imageURL,
		ArchivePath:    targetPath,
		ArchiveDigest:  copyResult.ArchiveDigest,
		SizeBytes:      copyResult.SizeBytes,
		ImportedAtUnix: m.now().Unix(),
	}
	if err := m.store.putImport(record); err != nil {
		return nil, fmt.Errorf("persist import record for %s: %w", imageURL, err)
	}
	logrus.Infof("OCI image import success: image=%s archive=%s size_bytes=%d digest=%s", imageURL, targetPath, copyResult.SizeBytes, copyResult.ArchiveDigest)
	return &ImportResult{
		ImageURL:       record.ImageURL,
		ArchivePath:    record.ArchivePath,
		ArchiveDigest:  record.ArchiveDigest,
		SizeBytes:      record.SizeBytes,
		ImportedAtUnix: record.ImportedAtUnix,
	}, nil
}

// HasImportedImage reports whether imageURL has a node-local imported archive
// registered. Callers use this to avoid registry/Nydus probing for imported refs.
func (m *Manager) HasImportedImage(imageURL string) (bool, error) {
	imageURL = strings.TrimSpace(imageURL)
	if imageURL == "" {
		return false, nil
	}
	rec, err := m.store.getImport(imageURL)
	if err != nil {
		return false, err
	}
	return rec != nil, nil
}

// ResolveImportedImageCacheKey returns the content-addressed mount cache key for
// a node-local imported image. The boolean result is false when imageURL is not
// imported on this node.
func (m *Manager) ResolveImportedImageCacheKey(imageURL string) (string, bool, error) {
	imageURL = strings.TrimSpace(imageURL)
	if imageURL == "" {
		return "", false, nil
	}
	rec, err := m.store.getImport(imageURL)
	if err != nil {
		return "", false, err
	}
	if rec == nil {
		return "", false, nil
	}
	if rec.ArchiveDigest == "" {
		return "", true, fmt.Errorf("imported image %s has no archive digest; re-import the image", imageURL)
	}
	return imageURL + "@" + rec.ArchiveDigest, true, nil
}

// ListImportedImages returns refs with node-local archive content.
func (m *Manager) ListImportedImages() ([]ImportedImageRecord, error) {
	records, err := m.store.listImports()
	if err != nil {
		return nil, fmt.Errorf("failed to list imported images: %w", err)
	}
	out := make([]ImportedImageRecord, 0, len(records))
	for _, rec := range records {
		if rec == nil || rec.ImageURL == "" {
			continue
		}
		out = append(out, *rec)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ImageURL < out[j].ImageURL
	})
	return out, nil
}

func (m *Manager) importedImage(ctx context.Context, imageURL string) (v1.Image, bool, error) {
	rec, err := m.store.getImport(imageURL)
	if err != nil {
		return nil, false, err
	}
	if rec == nil {
		return nil, false, nil
	}
	select {
	case <-ctx.Done():
		return nil, true, ctx.Err()
	default:
	}
	img, err := imageFromArchive(rec.ArchivePath, imageURL)
	if err != nil {
		return nil, true, fmt.Errorf("load imported image archive %s for %s: %w", rec.ArchivePath, imageURL, err)
	}
	return img, true, nil
}

func imageFromArchive(archivePath, imageURL string) (v1.Image, error) {
	if tag, err := name.NewTag(imageURL, name.WeakValidation); err == nil {
		return tarball.ImageFromPath(archivePath, &tag)
	}
	return tarball.ImageFromPath(archivePath, nil)
}

func importArchiveName(imageURL string) string {
	sum := sha256.Sum256([]byte(imageURL))
	return hex.EncodeToString(sum[:])
}

func copyArchiveAndComputeDigest(dst, src string) (*archiveCopyResult, error) {
	in, err := os.Open(src)
	if err != nil {
		return nil, fmt.Errorf("open archive %s: %w", src, err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return nil, fmt.Errorf("create import archive %s: %w", dst, err)
	}
	hasher := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(out, hasher), in)
	closeErr := out.Close()
	if copyErr != nil {
		return nil, fmt.Errorf("copy archive to %s: %w", dst, copyErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close import archive %s: %w", dst, closeErr)
	}
	return &archiveCopyResult{
		SizeBytes:     n,
		ArchiveDigest: "sha256:" + hex.EncodeToString(hasher.Sum(nil)),
	}, nil
}
