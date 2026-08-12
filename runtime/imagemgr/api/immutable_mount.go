package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const (
	maxImmutableLowerDirs          = 256
	maxImmutableBackingFilesystems = 32
	maxImmutablePathBytes          = 4096
	maxImmutableResourceIdentity   = 1024
)

var immutableFilesystemPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,63}$`)

func immutableMountDescriptor(mountPath, leaseID, filesystem, resourceIdentity string, lowerDirs, backingFilesystems []string) (*ImmutableMount, error) {
	if strings.TrimSpace(mountPath) != mountPath || strings.TrimSpace(leaseID) != leaseID || strings.TrimSpace(filesystem) != filesystem || strings.TrimSpace(resourceIdentity) != resourceIdentity {
		return nil, fmt.Errorf("immutable mount fields must use their canonical representation")
	}
	if !validImmutablePath(mountPath) || leaseID == "" || !immutableFilesystemPattern.MatchString(filesystem) || resourceIdentity == "" || len(resourceIdentity) > maxImmutableResourceIdentity || strings.ContainsAny(resourceIdentity, "\x00\n\r\t") || len(lowerDirs) == 0 || len(lowerDirs) > maxImmutableLowerDirs {
		return nil, fmt.Errorf("immutable mount requires an absolute path, lease, filesystem, resource identity, and lower dirs")
	}
	canonicalLowers := make([]string, 0, len(lowerDirs))
	seenLowers := make(map[string]struct{}, len(lowerDirs))
	for _, lowerDir := range lowerDirs {
		if strings.TrimSpace(lowerDir) != lowerDir || !validImmutablePath(lowerDir) || strings.ContainsAny(lowerDir, `,:\`) {
			return nil, fmt.Errorf("immutable lower dir must be a canonical absolute path: %q", lowerDir)
		}
		if _, duplicate := seenLowers[lowerDir]; duplicate {
			return nil, fmt.Errorf("immutable lower dir is duplicated: %q", lowerDir)
		}
		seenLowers[lowerDir] = struct{}{}
		canonicalLowers = append(canonicalLowers, lowerDir)
	}
	if len(backingFilesystems) > maxImmutableBackingFilesystems {
		return nil, fmt.Errorf("immutable mount has too many backing filesystems: %d", len(backingFilesystems))
	}
	filesystems := make([]string, 0, len(backingFilesystems))
	seen := make(map[string]struct{}, len(backingFilesystems))
	for _, item := range backingFilesystems {
		if strings.TrimSpace(item) != item || !immutableFilesystemPattern.MatchString(item) {
			return nil, fmt.Errorf("immutable backing filesystem is malformed: %q", item)
		}
		if _, duplicate := seen[item]; duplicate {
			return nil, fmt.Errorf("immutable backing filesystem is duplicated: %q", item)
		}
		seen[item] = struct{}{}
		filesystems = append(filesystems, item)
	}
	sort.Strings(filesystems)
	identitySubject := struct {
		ResourceIdentity   string   `json:"resource_identity"`
		MountPath          string   `json:"mount_path"`
		Filesystem         string   `json:"filesystem"`
		BackingFilesystems []string `json:"backing_filesystems,omitempty"`
		LowerDirs          []string `json:"lower_dirs"`
	}{resourceIdentity, mountPath, filesystem, filesystems, canonicalLowers}
	payload, err := json.Marshal(identitySubject)
	if err != nil {
		return nil, fmt.Errorf("marshal immutable mount identity: %w", err)
	}
	sum := sha256.Sum256(payload)
	return &ImmutableMount{
		Identity: "sha256:" + hex.EncodeToString(sum[:]), EffectiveRoot: mountPath, Filesystem: filesystem,
		BackingFilesystems: filesystems, LowerDirs: canonicalLowers, Readonly: true, LeaseID: leaseID,
	}, nil
}

func validImmutablePath(value string) bool {
	return value != "" && len(value) <= maxImmutablePathBytes && filepath.IsAbs(value) && filepath.Clean(value) == value && !strings.ContainsAny(value, "\x00\n\r\t")
}
