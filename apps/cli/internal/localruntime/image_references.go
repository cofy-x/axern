package localruntime

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	localImageReferencesVersion = 1
	maxLocalImageReferences     = 4096
)

type localImageReference struct {
	CanonicalRef     string    `json:"canonical_ref"`
	ImmutableRef     string    `json:"immutable_ref"`
	GenerationDigest string    `json:"generation_digest"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type localImageReferenceIndex struct {
	Version    int                            `json:"version"`
	References map[string]localImageReference `json:"references"`
}

func saveLocalImageReference(dir, sourceRef, canonicalRef, immutableRef, generationDigest string) error {
	sourceRef = strings.TrimSpace(sourceRef)
	canonicalRef = strings.TrimSpace(canonicalRef)
	immutableRef = strings.TrimSpace(immutableRef)
	generationDigest = strings.TrimSpace(generationDigest)
	if sourceRef == "" || canonicalRef == "" || immutableRef == "" || generationDigest == "" {
		return fmt.Errorf("source, canonical, immutable, and generation image identities are required")
	}
	if !validLocalGeneration(generationDigest) || !strings.HasSuffix(immutableRef, "@"+generationDigest) {
		return fmt.Errorf("immutable image ref %q does not match generation %q", immutableRef, generationDigest)
	}

	path := filepath.Join(dir, "image-references.json")
	index, err := loadLocalImageReferences(path)
	if errors.Is(err, os.ErrNotExist) {
		index = &localImageReferenceIndex{Version: localImageReferencesVersion, References: map[string]localImageReference{}}
	} else if err != nil {
		return err
	}
	newAliases := 0
	if _, exists := index.References[sourceRef]; !exists {
		newAliases++
	}
	if canonicalRef != sourceRef {
		if _, exists := index.References[canonicalRef]; !exists {
			newAliases++
		}
	}
	if len(index.References)+newAliases > maxLocalImageReferences {
		return fmt.Errorf("local image reference index reached %d entries", maxLocalImageReferences)
	}
	record := localImageReference{
		CanonicalRef: canonicalRef, ImmutableRef: immutableRef,
		GenerationDigest: generationDigest, UpdatedAt: time.Now().UTC(),
	}
	index.References[sourceRef] = record
	index.References[canonicalRef] = record
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeAtomic(path, data, 0o600)
}

// ResolveLocalImageReference resolves a mutable image ref to the immutable
// generation selected by the latest successful local image load. The node's
// imported generation store remains authoritative; this index is only the
// local CLI pointer used to avoid a registry lookup at the control plane.
func ResolveLocalImageReference(dir, imageRef string) (string, bool, error) {
	imageRef = strings.TrimSpace(imageRef)
	if imageRef == "" {
		return "", false, nil
	}
	index, err := loadLocalImageReferences(filepath.Join(dir, "image-references.json"))
	if errors.Is(err, os.ErrNotExist) {
		return imageRef, false, nil
	}
	if err != nil {
		return "", false, err
	}
	record, ok := index.References[imageRef]
	if !ok {
		return imageRef, false, nil
	}
	return record.ImmutableRef, true, nil
}

func loadLocalImageReferences(path string) (*localImageReferenceIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var index localImageReferenceIndex
	if err := decoder.Decode(&index); err != nil {
		return nil, fmt.Errorf("parse local image reference index: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse local image reference index: multiple JSON values")
	}
	if index.Version != localImageReferencesVersion {
		return nil, fmt.Errorf("unsupported local image reference index version %d", index.Version)
	}
	if len(index.References) > maxLocalImageReferences {
		return nil, fmt.Errorf("local image reference index exceeds %d entries", maxLocalImageReferences)
	}
	if index.References == nil {
		return nil, fmt.Errorf("local image reference index is missing references")
	}
	for alias, record := range index.References {
		if strings.TrimSpace(alias) == "" || strings.TrimSpace(record.CanonicalRef) == "" ||
			strings.TrimSpace(record.ImmutableRef) == "" || !validLocalGeneration(record.GenerationDigest) ||
			!strings.HasSuffix(record.ImmutableRef, "@"+record.GenerationDigest) || record.UpdatedAt.IsZero() {
			return nil, fmt.Errorf("local image reference index contains an invalid record for %q", alias)
		}
	}
	return &index, nil
}

func validLocalGeneration(value string) bool {
	if len(value) != len("sha256:")+64 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	for _, char := range strings.TrimPrefix(value, "sha256:") {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return false
		}
	}
	return true
}
