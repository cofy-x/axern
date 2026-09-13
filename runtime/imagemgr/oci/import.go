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

const importedCacheKeyPrefix = "local-import@"

// ImportResult describes a completed node-local image import.
type ImportResult struct {
	SourceRef       string
	ImageURL        string
	ContentDigest   string
	ArchivePath     string
	ArchiveDigest   string
	PlatformOS      string
	PlatformArch    string
	PlatformVariant string
	SizeBytes       int64
	ImportedAtUnix  int64
	Reused          bool
}

// ImportImage streams one Docker archive into the content-addressed local OCI
// store, then atomically moves the mutable ref to the imported manifest digest.
func (m *Manager) ImportImage(ctx context.Context, sourceRef string, archive io.Reader) (*ImportResult, error) {
	if archive == nil {
		return nil, fmt.Errorf("archive stream is required")
	}
	sourceRef = strings.TrimSpace(sourceRef)
	canonicalRef, err := canonicalImageRef(sourceRef)
	if err != nil {
		return nil, err
	}

	unlockImage := m.acquireImageLock(canonicalRef)
	defer unlockImage()

	stagingDir := filepath.Join(m.importsDir, "staging")
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return nil, fmt.Errorf("create import staging directory: %w", err)
	}
	tmp, err := os.CreateTemp(stagingDir, "import-*.tar")
	if err != nil {
		return nil, fmt.Errorf("create import staging file: %w", err)
	}
	tmpPath := tmp.Name()
	removeTmp := true
	defer func() {
		_ = tmp.Close()
		if removeTmp {
			_ = os.Remove(tmpPath)
		}
	}()

	hasher := sha256.New()
	size, err := copyWithContext(ctx, io.MultiWriter(tmp, hasher), archive)
	if err != nil {
		return nil, fmt.Errorf("receive image archive: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return nil, fmt.Errorf("sync image archive: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close image archive: %w", err)
	}
	archiveDigest := "sha256:" + hex.EncodeToString(hasher.Sum(nil))

	manifest, err := tarball.LoadManifest(func() (io.ReadCloser, error) { return os.Open(tmpPath) })
	if err != nil {
		return nil, fmt.Errorf("invalid Docker archive: %w", err)
	}
	if len(manifest) != 1 {
		return nil, fmt.Errorf("Docker archive must contain exactly one image, found %d", len(manifest))
	}
	img, err := tarball.ImageFromPath(tmpPath, nil)
	if err != nil {
		return nil, fmt.Errorf("load Docker archive image: %w", err)
	}
	layers, err := img.Layers()
	if err != nil {
		return nil, fmt.Errorf("read imported image layers: %w", err)
	}
	if len(layers) == 0 {
		return nil, fmt.Errorf("imported image has no layers")
	}
	digest, err := img.Digest()
	if err != nil {
		return nil, fmt.Errorf("read imported manifest digest: %w", err)
	}
	config, err := img.ConfigFile()
	if err != nil {
		return nil, fmt.Errorf("read imported image platform: %w", err)
	}
	platform := v1.Platform{OS: config.OS, Architecture: config.Architecture, Variant: config.Variant}
	if err := m.validateImportPlatform(platform); err != nil {
		return nil, err
	}

	contentDigest := digest.String()
	archivePath, err := m.importBlobPath(contentDigest)
	if err != nil {
		return nil, err
	}
	reused := false
	var content *importedContentRecord
	if existing, err := m.store.getImportContent(contentDigest); err != nil {
		return nil, fmt.Errorf("query imported content %s: %w", contentDigest, err)
	} else if existing != nil {
		if filepath.Clean(existing.ArchivePath) != filepath.Clean(archivePath) {
			return nil, fmt.Errorf("imported content %s has invalid archive path %q", contentDigest, existing.ArchivePath)
		}
		if info, statErr := os.Stat(archivePath); statErr == nil && info.Mode().IsRegular() {
			reused = true
			content = existing
		} else {
			if err := os.MkdirAll(filepath.Dir(archivePath), 0755); err != nil {
				return nil, fmt.Errorf("recreate import blob directory: %w", err)
			}
			if err := os.Rename(tmpPath, archivePath); err != nil {
				return nil, fmt.Errorf("restore imported content %s: %w", contentDigest, err)
			}
			removeTmp = false
			if err := syncDirectory(filepath.Dir(archivePath)); err != nil {
				return nil, err
			}
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(archivePath), 0755); err != nil {
			return nil, fmt.Errorf("create import blob directory: %w", err)
		}
		if err := os.Rename(tmpPath, archivePath); err != nil {
			return nil, fmt.Errorf("publish imported content %s: %w", contentDigest, err)
		}
		removeTmp = false
		if err := syncDirectory(filepath.Dir(archivePath)); err != nil {
			return nil, err
		}
	}

	importedAt := m.now().Unix()
	if content == nil {
		content = &importedContentRecord{
			ContentDigest: contentDigest, ArchivePath: archivePath, ArchiveDigest: archiveDigest,
			PlatformOS: platform.OS, PlatformArch: platform.Architecture, PlatformVariant: platform.Variant,
			SizeBytes: size, ImportedAtUnix: importedAt,
		}
	}
	ref := &importedRefRecord{ImageURL: canonicalRef, ContentDigest: contentDigest}
	if err := m.store.putImport(ref, content); err != nil {
		return nil, fmt.Errorf("publish imported ref %s: %w", canonicalRef, err)
	}
	if err := m.pruneImportedContents(); err != nil {
		logrus.WithError(err).Warn("prune unreferenced imported contents")
	}
	logrus.Infof("OCI image import success: ref=%s content=%s size_bytes=%d archive_digest=%s reused=%t", canonicalRef, contentDigest, size, archiveDigest, reused)
	result := importResult(sourceRef, canonicalRef, content, reused)
	result.ArchiveDigest, result.SizeBytes = archiveDigest, size
	return result, nil
}

// ImportImageArchive is an internal file adapter used by tests and trusted
// node-local callers. The HTTP API accepts archive bytes, never host paths.
func (m *Manager) ImportImageArchive(ctx context.Context, imageURL, archivePath string) (*ImportResult, error) {
	file, err := os.Open(strings.TrimSpace(archivePath))
	if err != nil {
		return nil, fmt.Errorf("open archive %s: %w", archivePath, err)
	}
	defer file.Close()
	return m.ImportImage(ctx, imageURL, file)
}

func (m *Manager) HasImportedImage(imageURL string) (bool, error) {
	canonicalRef, err := canonicalImageRef(imageURL)
	if err != nil {
		return false, nil
	}
	rec, err := m.store.getImport(canonicalRef)
	return rec != nil, err
}

func (m *Manager) ResolveImportedImageCacheKey(imageURL string) (string, bool, error) {
	canonicalRef, err := canonicalImageRef(imageURL)
	if err != nil {
		return "", false, nil
	}
	rec, err := m.store.getImport(canonicalRef)
	if err != nil || rec == nil {
		return "", false, err
	}
	return importedCacheKey(rec.ContentDigest), true, nil
}

// ResolveImageCacheKey canonicalizes imageURL and returns the current immutable
// content key when the ref is backed by a local import.
func (m *Manager) ResolveImageCacheKey(imageURL string) (string, string, bool, error) {
	canonicalRef, err := canonicalImageRef(imageURL)
	if err != nil {
		return "", "", false, err
	}
	record, err := m.store.getImport(canonicalRef)
	if err != nil {
		return "", "", false, err
	}
	if record != nil {
		return canonicalRef, importedCacheKey(record.ContentDigest), true, nil
	}
	if at := strings.LastIndex(canonicalRef, "@"); at >= 0 {
		digest := canonicalRef[at+1:]
		content, err := m.store.getImportContent(digest)
		if err != nil {
			return "", "", false, err
		}
		if content != nil {
			return canonicalRef, importedCacheKey(digest), true, nil
		}
	}
	return canonicalRef, canonicalRef, false, nil
}

// HasImportedContent reports whether cacheKey names a retained immutable
// imported content. It does not require that any mutable ref still selects it.
func (m *Manager) HasImportedContent(cacheKey string) (bool, error) {
	digest, ok := importedDigestFromCacheKey(cacheKey)
	if !ok {
		return false, nil
	}
	record, err := m.store.getImportContent(digest)
	return record != nil, err
}

func (m *Manager) ListImportedImages() ([]ImportedImageRecord, error) {
	records, err := m.store.listImports()
	if err != nil {
		return nil, fmt.Errorf("list imported images: %w", err)
	}
	out := make([]ImportedImageRecord, 0, len(records))
	for _, rec := range records {
		if rec != nil {
			out = append(out, *rec)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ImageURL < out[j].ImageURL })
	return out, nil
}

func (m *Manager) importedImage(ctx context.Context, imageURL, cacheKey string) (v1.Image, bool, error) {
	var content *importedContentRecord
	var err error
	if digest, ok := importedDigestFromCacheKey(cacheKey); ok {
		content, err = m.store.getImportContent(digest)
	} else {
		canonicalRef, canonicalErr := canonicalImageRef(imageURL)
		if canonicalErr != nil {
			return nil, false, nil
		}
		var rec *ImportedImageRecord
		rec, err = m.store.getImport(canonicalRef)
		if rec != nil {
			content = &importedContentRecord{ContentDigest: rec.ContentDigest, ArchivePath: rec.ArchivePath}
		}
	}
	if err != nil || content == nil {
		return nil, false, err
	}
	expectedPath, err := m.importBlobPath(content.ContentDigest)
	if err != nil || filepath.Clean(content.ArchivePath) != filepath.Clean(expectedPath) {
		if err == nil {
			err = fmt.Errorf("content archive path %q differs from %q", content.ArchivePath, expectedPath)
		}
		return nil, true, fmt.Errorf("invalid imported content %s: %w", content.ContentDigest, err)
	}
	select {
	case <-ctx.Done():
		return nil, true, ctx.Err()
	default:
	}
	img, err := tarball.ImageFromPath(content.ArchivePath, nil)
	if err != nil {
		return nil, true, fmt.Errorf("load imported content %s: %w", content.ContentDigest, err)
	}
	return img, true, nil
}

func canonicalImageRef(raw string) (string, error) {
	ref, err := name.ParseReference(strings.TrimSpace(raw), name.WeakValidation)
	if err != nil {
		return "", fmt.Errorf("invalid image ref %q: %w", raw, err)
	}
	return ref.Name(), nil
}

// ImmutableImageRef returns the repository-scoped digest reference for an
// imported content. The mutable tag remains a convenience pointer; callers
// that cross the control-plane boundary must use this immutable identity so
// resolution never requires a registry lookup.
func ImmutableImageRef(canonicalRef, contentDigest string) (string, error) {
	ref, err := name.ParseReference(strings.TrimSpace(canonicalRef), name.WeakValidation)
	if err != nil {
		return "", fmt.Errorf("invalid canonical image ref %q: %w", canonicalRef, err)
	}
	digestValue, err := v1.NewHash(strings.TrimSpace(contentDigest))
	if err != nil {
		return "", fmt.Errorf("invalid imported content digest %q: %w", contentDigest, err)
	}
	return ref.Context().Digest(digestValue.String()).Name(), nil
}

func importedCacheKey(digest string) string { return importedCacheKeyPrefix + digest }

func importedDigestFromCacheKey(cacheKey string) (string, bool) {
	digest, ok := strings.CutPrefix(cacheKey, importedCacheKeyPrefix)
	return digest, ok && strings.HasPrefix(digest, "sha256:")
}

func (m *Manager) importBlobPath(digest string) (string, error) {
	algorithm, value, ok := strings.Cut(digest, ":")
	if !ok || algorithm != "sha256" || len(value) != 64 {
		return "", fmt.Errorf("invalid imported content digest %q", digest)
	}
	return filepath.Join(m.importsDir, "blobs", algorithm, value+".tar"), nil
}

func (m *Manager) validateImportPlatform(platform v1.Platform) error {
	if m.targetPlatform == nil {
		return nil
	}
	if platform.OS == "" || platform.Architecture == "" {
		return fmt.Errorf("imported image is missing OS or architecture metadata")
	}
	if platform.OS != m.targetPlatform.OS || platform.Architecture != m.targetPlatform.Architecture ||
		(m.targetPlatform.Variant != "" && platform.Variant != m.targetPlatform.Variant) {
		return fmt.Errorf("image platform %s/%s%s does not match node platform %s/%s%s", platform.OS, platform.Architecture, formatVariant(platform.Variant), m.targetPlatform.OS, m.targetPlatform.Architecture, formatVariant(m.targetPlatform.Variant))
	}
	return nil
}

func formatVariant(variant string) string {
	if variant == "" {
		return ""
	}
	return "/" + variant
}

func importResult(sourceRef, canonicalRef string, content *importedContentRecord, reused bool) *ImportResult {
	return &ImportResult{
		SourceRef: sourceRef, ImageURL: canonicalRef, ContentDigest: content.ContentDigest,
		ArchivePath: content.ArchivePath, ArchiveDigest: content.ArchiveDigest,
		PlatformOS: content.PlatformOS, PlatformArch: content.PlatformArch, PlatformVariant: content.PlatformVariant,
		SizeBytes: content.SizeBytes, ImportedAtUnix: content.ImportedAtUnix, Reused: reused,
	}
}

func copyWithContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buffer := make([]byte, 128*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := src.Read(buffer)
		if n > 0 {
			written, writeErr := dst.Write(buffer[:n])
			total += int64(written)
			if writeErr != nil {
				return total, writeErr
			}
			if written != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return total, nil
		}
		if readErr != nil {
			return total, readErr
		}
	}
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open directory %s for sync: %w", path, err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync directory %s: %w", path, err)
	}
	return nil
}

func (m *Manager) pruneImportedContents() error {
	contents, err := m.store.listImportContents()
	if err != nil {
		return err
	}
	for _, content := range contents {
		expectedPath, pathErr := m.importBlobPath(content.ContentDigest)
		if pathErr != nil || filepath.Clean(content.ArchivePath) != filepath.Clean(expectedPath) {
			if pathErr == nil {
				pathErr = fmt.Errorf("archive path %q differs from %q", content.ArchivePath, expectedPath)
			}
			return fmt.Errorf("invalid imported content %s: %w", content.ContentDigest, pathErr)
		}
		cacheKey := importedCacheKey(content.ContentDigest)
		unlock := m.acquireImageLock(cacheKey)
		referenced, err := m.store.importContentReferenced(content.ContentDigest)
		if err != nil {
			unlock()
			return err
		}
		if referenced {
			unlock()
			continue
		}
		if m.getContainer(cacheKey) != nil {
			unlock()
			continue
		}
		if mount, err := m.store.getMount(cacheKey); err != nil {
			unlock()
			return err
		} else if mount != nil {
			unlock()
			continue
		}
		if err := m.store.deleteImportContent(content.ContentDigest); err != nil {
			unlock()
			return err
		}
		if err := os.Remove(content.ArchivePath); err != nil && !os.IsNotExist(err) {
			unlock()
			return fmt.Errorf("remove unreferenced imported content %s: %w", content.ContentDigest, err)
		}
		unlock()
	}
	return nil
}

func (m *Manager) reconcileImportedState() error {
	stagingDir := filepath.Join(m.importsDir, "staging")
	if err := os.RemoveAll(stagingDir); err != nil {
		return fmt.Errorf("clear import staging: %w", err)
	}
	if err := os.MkdirAll(stagingDir, 0755); err != nil {
		return fmt.Errorf("recreate import staging: %w", err)
	}
	if err := m.pruneImportedContents(); err != nil {
		return err
	}
	contents, err := m.store.listImportContents()
	if err != nil {
		return err
	}
	known := make(map[string]struct{}, len(contents))
	for _, content := range contents {
		expectedPath, pathErr := m.importBlobPath(content.ContentDigest)
		if pathErr != nil || filepath.Clean(content.ArchivePath) != filepath.Clean(expectedPath) {
			if pathErr == nil {
				pathErr = fmt.Errorf("archive path %q differs from %q", content.ArchivePath, expectedPath)
			}
			return fmt.Errorf("invalid imported content %s: %w", content.ContentDigest, pathErr)
		}
		known[filepath.Clean(content.ArchivePath)] = struct{}{}
	}
	blobsDir := filepath.Join(m.importsDir, "blobs")
	return filepath.WalkDir(blobsDir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if _, ok := known[filepath.Clean(path)]; ok {
			return nil
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove orphan import blob %s: %w", path, err)
		}
		return nil
	})
}
