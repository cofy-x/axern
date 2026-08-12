package rootfsview

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/durablefile"
	"github.com/sirupsen/logrus"
)

const (
	projectionViewDir = "projections"
	runcViewDir       = "runc"

	maxImmutableLowerDirs          = 256
	maxImmutableBackingFilesystems = 32
	maxImmutablePathBytes          = 4096
	maxImmutableLeaseIDBytes       = 512
	maxProjectionManifestBytes     = 1 << 20
)

var (
	mountInfoOctalEscapePattern = regexp.MustCompile(`\\[0-7]{3}`)
	immutableIdentityPattern    = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	immutableFilesystemPattern  = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]{0,63}$`)
)

func validateOverlayPath(name, candidate string) error {
	if err := validateCanonicalAbsolutePath(name, candidate); err != nil {
		return err
	}
	if strings.ContainsAny(candidate, `,:\`) || strings.ContainsAny(candidate, "\x00\n\r\t") {
		return fmt.Errorf("%s contains an unsupported mount-option delimiter: %s", name, candidate)
	}
	return nil
}

func validateCanonicalAbsolutePath(name, candidate string) error {
	if candidate == "" || !filepath.IsAbs(candidate) || filepath.Clean(candidate) != candidate {
		return fmt.Errorf("%s must be a canonical absolute path: %s", name, candidate)
	}
	if strings.ContainsAny(candidate, "\x00\n\r\t") {
		return fmt.Errorf("%s contains a control character", name)
	}
	return nil
}

// View is a prepared container rootfs view. Prepared views are active
// container snapshots and must be removed when the container is deleted.
type View struct {
	RootDir  string
	Prepared bool
}

type TargetKind uint8

const (
	TargetRegularFile TargetKind = iota + 1
	TargetDirectory
)

type MountTarget struct {
	Destination string
	Kind        TargetKind
}

type RootfsBackingFacts struct {
	EffectiveRoot       string                    `json:"effective_root"`
	MountID             int                       `json:"mount_id"`
	Mountpoint          string                    `json:"mountpoint"`
	MountRoot           string                    `json:"mount_root"`
	FSType              string                    `json:"fs_type"`
	Source              string                    `json:"source"`
	Readonly            bool                      `json:"readonly"`
	LowerDirs           []string                  `json:"lower_dirs"`
	EffectiveLowerChain []RootfsBackingLayerFacts `json:"effective_lower_chain"`
}

// ImmutableMountDescriptor is the bounded hand-off from a rootfs source owner
// to runtime projection. It contains no image-format-specific state and cannot
// grow into a recursive backing graph.
type ImmutableMountDescriptor struct {
	Identity           string   `json:"identity"`
	EffectiveRoot      string   `json:"effective_root"`
	Filesystem         string   `json:"filesystem"`
	BackingFilesystems []string `json:"backing_filesystems,omitempty"`
	LowerDirs          []string `json:"lower_dirs"`
	Readonly           bool     `json:"readonly"`
	LeaseID            string   `json:"lease_id,omitempty"`
}

func (descriptor ImmutableMountDescriptor) HasFilesystem(filesystem string) bool {
	if strings.EqualFold(descriptor.Filesystem, filesystem) {
		return true
	}
	for _, item := range descriptor.BackingFilesystems {
		if strings.EqualFold(item, filesystem) {
			return true
		}
	}
	return false
}

// ImmutableMountDescriptor converts the one-time local-source observation to
// the same immutable contract that imagemgr emits for image-backed rootfses.
func (facts RootfsBackingFacts) ImmutableMountDescriptor(leaseID string) ImmutableMountDescriptor {
	lowerDirs := append([]string(nil), facts.LowerDirs...)
	if len(lowerDirs) == 0 && facts.EffectiveRoot != "" {
		lowerDirs = []string{facts.EffectiveRoot}
	}
	filesystems := []string{strings.ToLower(strings.TrimSpace(facts.FSType))}
	for _, layer := range facts.EffectiveLowerChain {
		filesystems = append(filesystems, strings.ToLower(strings.TrimSpace(layer.FSType)))
	}
	filesystems = uniqueSortedStrings(filesystems)
	payload, _ := json.Marshal(facts)
	digest := sha256.Sum256(payload)
	return ImmutableMountDescriptor{
		Identity: fmt.Sprintf("sha256:%x", digest[:]), EffectiveRoot: filepath.Clean(facts.EffectiveRoot),
		Filesystem: strings.ToLower(strings.TrimSpace(facts.FSType)), BackingFilesystems: filesystems,
		LowerDirs: lowerDirs, Readonly: true, LeaseID: strings.TrimSpace(leaseID),
	}
}

func uniqueSortedStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, duplicate := seen[value]; duplicate {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// RootfsBackingLayerFacts identifies the mount that provides one directory in
// the effective lower chain. The path alone is not an identity: an image
// manager may remount a different filesystem at the same pathname.
type RootfsBackingLayerFacts struct {
	Path       string `json:"path"`
	MountID    int    `json:"mount_id"`
	Mountpoint string `json:"mountpoint"`
	MountRoot  string `json:"mount_root"`
	FSType     string `json:"fs_type"`
	Source     string `json:"source"`
	Readonly   bool   `json:"readonly"`
}

// HasFilesystem reports whether the effective root or any directory that
// contributes to its lower chain is backed by the named filesystem.
func (facts RootfsBackingFacts) HasFilesystem(fsType string) bool {
	if strings.EqualFold(facts.FSType, fsType) {
		return true
	}
	for _, layer := range facts.EffectiveLowerChain {
		if strings.EqualFold(layer.FSType, fsType) {
			return true
		}
	}
	return false
}

type Request struct {
	RootDir                    string
	Readonly                   bool
	RuntimeName                string
	NeedsHostWritableRootfs    bool
	ImmutableMount             ImmutableMountDescriptor
	Targets                    []MountTarget
	EphemeralStorageLimitBytes int64
	ProjectID                  uint32
}

// Provider owns the lifecycle of active sandbox-private rootfs views.
type Provider interface {
	Prepare(ctx context.Context, containerID string, request Request) (View, error)
	Remove(ctx context.Context, containerID string) error
}

type PersistentReconciler interface {
	ReconcilePersistentViews(context.Context, string, map[string]struct{}) error
}

type overlayProvider struct {
	filestoreDir string
}

type overlayView struct {
	LowerDirs []string
	UpperDir  string
	WorkDir   string
	MergedDir string
}

func NewOverlayProvider(filestoreDir string) Provider {
	return &overlayProvider{filestoreDir: filestoreDir}
}

func (p *overlayProvider) Prepare(_ context.Context, containerID string, request Request) (View, error) {
	if !validContainerID(containerID) {
		return View{}, fmt.Errorf("invalid container ID for rootfs projection %q", containerID)
	}
	if strings.TrimSpace(request.RootDir) == "" {
		return View{}, fmt.Errorf("rootfs path is required")
	}
	rootInfo, err := os.Stat(request.RootDir)
	if err != nil {
		return View{}, fmt.Errorf("stat rootfs %s: %w", request.RootDir, err)
	}
	if !rootInfo.IsDir() {
		return View{}, fmt.Errorf("rootfs %s is not a directory", request.RootDir)
	}

	missing, err := inspectMountTargets(request.RootDir, request.Targets)
	if err != nil {
		return View{}, err
	}
	if len(missing) == 0 && !request.NeedsHostWritableRootfs {
		return View{}, nil
	}
	if p.filestoreDir == "" {
		return View{}, fmt.Errorf("private rootfs view requires runtime filestore_dir: %s", request.RootDir)
	}

	if err := ValidateImmutableMountDescriptor(request.ImmutableMount, request.RootDir); err != nil {
		return View{}, err
	}
	lowerDirs := append([]string(nil), request.ImmutableMount.LowerDirs...)
	viewClass := projectionViewDir
	if request.NeedsHostWritableRootfs {
		viewClass = runcViewDir
	}
	view := overlayViewForContainer(containerID, p.filestoreDir, viewClass, lowerDirs)
	if err := initializeOverlayView(view); err != nil {
		return View{}, err
	}
	if err := seedMountTargets(request.RootDir, view.UpperDir, missing); err != nil {
		_ = cleanupOverlayView(filepath.Dir(view.MergedDir))
		return View{}, err
	}
	if request.NeedsHostWritableRootfs {
		if err := applyProjectQuota(p.filestoreDir, view.UpperDir, request.ProjectID, request.EphemeralStorageLimitBytes); err != nil {
			metrics.RecordEphemeralStorageOperation(request.RuntimeName, "project_quota", "failure")
			_ = cleanupOverlayViewWithProject(filepath.Dir(view.MergedDir), p.filestoreDir, request.ProjectID)
			return View{}, err
		}
		metrics.RecordEphemeralStorageOperation(request.RuntimeName, "project_quota", "success")
	}
	if err := mountOverlayView(view); err != nil {
		result := "failure"
		if errors.Is(err, syscall.ENOSPC) {
			result = "enospc"
		}
		metrics.RecordEphemeralStorageOperation(request.RuntimeName, "projection_mount", result)
		_ = cleanupOverlayViewWithProject(filepath.Dir(view.MergedDir), p.filestoreDir, request.ProjectID)
		return View{}, err
	}
	metrics.RecordEphemeralStorageOperation(request.RuntimeName, "projection_mount", "success")
	if err := writeProjectionManifest(filepath.Dir(view.MergedDir), request); err != nil {
		_ = cleanupOverlayViewWithProject(filepath.Dir(view.MergedDir), p.filestoreDir, request.ProjectID)
		return View{}, err
	}

	logrus.WithFields(logrus.Fields{
		"container_id":    containerID,
		"lowerdir_count":  len(view.LowerDirs),
		"rootfs":          view.MergedDir,
		"storage":         p.filestoreDir,
		"missing_targets": len(missing),
		"root_readonly":   request.Readonly,
		"runtime":         request.RuntimeName,
		"backing_fs":      request.ImmutableMount.Filesystem,
	}).Debug("prepared sandbox-private rootfs view")
	return View{RootDir: view.MergedDir, Prepared: true}, nil
}

func inspectMountTargets(root string, targets []MountTarget) ([]MountTarget, error) {
	missing := make([]MountTarget, 0)
	for _, target := range targets {
		destination := strings.TrimSpace(target.Destination)
		if destination == "" || destination != target.Destination || path.Clean(destination) != destination {
			return nil, fmt.Errorf("bind mount target %q must be a canonical container path", target.Destination)
		}
		if !path.IsAbs(destination) {
			return nil, fmt.Errorf("bind mount target %q must be an absolute container path", target.Destination)
		}
		if destination == "/" {
			if target.Kind != TargetDirectory {
				return nil, fmt.Errorf("bind mount target / requires a directory source")
			}
			continue
		}
		if target.Kind != TargetRegularFile && target.Kind != TargetDirectory {
			return nil, fmt.Errorf("bind mount target %q has unsupported source type", destination)
		}
		ready, err := inspectMountTarget(root, destination, target.Kind)
		if err != nil {
			return nil, err
		}
		if !ready {
			missing = append(missing, target)
		}
	}
	return missing, nil
}

func inspectMountTarget(root, destination string, kind TargetKind) (bool, error) {
	parts := strings.Split(strings.TrimPrefix(destination, "/"), "/")
	current := filepath.Clean(root)
	for index, part := range parts {
		current = filepath.Join(current, filepath.FromSlash(part))
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("inspect bind mount target %q: %w", destination, err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return false, fmt.Errorf("bind mount target %q contains a symlink", destination)
		}
		if index < len(parts)-1 {
			if !info.IsDir() {
				return false, fmt.Errorf("bind mount target %q has a non-directory parent", destination)
			}
			continue
		}
		switch kind {
		case TargetRegularFile:
			if !info.Mode().IsRegular() {
				return false, fmt.Errorf("bind mount target %q is not a regular file", destination)
			}
		case TargetDirectory:
			if !info.IsDir() {
				return false, fmt.Errorf("bind mount target %q is not a directory", destination)
			}
		}
	}
	return true, nil
}

func seedMountTargets(lowerRoot, upperRoot string, targets []MountTarget) error {
	for _, target := range targets {
		parts := strings.Split(strings.TrimPrefix(target.Destination, "/"), "/")
		parentCount := len(parts)
		if target.Kind == TargetRegularFile {
			parentCount--
		}
		for index := 0; index < parentCount; index++ {
			relative := filepath.Join(parts[:index+1]...)
			if err := createProjectionDirectory(filepath.Join(lowerRoot, relative), filepath.Join(upperRoot, relative)); err != nil {
				return fmt.Errorf("seed directory target %q: %w", target.Destination, err)
			}
		}
		if target.Kind == TargetRegularFile {
			targetPath := filepath.Join(upperRoot, filepath.Join(parts...))
			if err := os.MkdirAll(filepath.Dir(targetPath), 0755); err != nil {
				return err
			}
			file, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
			if err != nil && !os.IsExist(err) {
				return fmt.Errorf("seed file target %q: %w", target.Destination, err)
			}
			if err == nil {
				if err := file.Close(); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func createProjectionDirectory(lowerPath, upperPath string) error {
	if info, err := os.Lstat(upperPath); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("projection path %s is not a directory", upperPath)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	mode := os.FileMode(0755)
	uid, gid := 0, 0
	if info, err := os.Lstat(lowerPath); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("lower projection parent %s is not a safe directory", lowerPath)
		}
		mode = info.Mode().Perm()
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			uid, gid = int(stat.Uid), int(stat.Gid)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.Mkdir(upperPath, mode); err != nil {
		return err
	}
	if err := os.Chown(upperPath, uid, gid); err != nil {
		return fmt.Errorf("copy projection directory ownership: %w", err)
	}
	return os.Chmod(upperPath, mode)
}

func (p *overlayProvider) Remove(_ context.Context, containerID string) error {
	if !validContainerID(containerID) {
		return fmt.Errorf("invalid container ID for rootfs projection %q", containerID)
	}
	if p.filestoreDir == "" {
		return nil
	}
	var result error
	for _, class := range []string{projectionViewDir, runcViewDir} {
		if err := cleanupPersistedOverlayView(filepath.Join(p.filestoreDir, class, containerID), p.filestoreDir); err != nil {
			result = errors.Join(result, fmt.Errorf("cleanup %s rootfs view: %w", class, err))
		}
	}
	return result
}

func validContainerID(value string) bool {
	if value == "" || value == "." || filepath.Base(value) != value {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '.' || character == '_' || character == '-' || character == '+' {
			continue
		}
		return false
	}
	return true
}

func ValidateImmutableMountDescriptor(descriptor ImmutableMountDescriptor, rootDir string) error {
	if err := ValidateImmutableMountDescriptorContract(descriptor, rootDir); err != nil {
		return err
	}
	if err := validateImmutableDirectory("immutable mount effective root", descriptor.EffectiveRoot); err != nil {
		return err
	}
	for _, lowerDir := range descriptor.LowerDirs {
		if err := validateImmutableDirectory("immutable lower dir", lowerDir); err != nil {
			return err
		}
	}
	return nil
}

// ValidateImmutableMountDescriptorContract validates the bounded hand-off
// without probing host state. The source consumer uses it at transport/cache
// boundaries; projection calls ValidateImmutableMountDescriptor immediately
// before mounting to additionally prove the paths still exist.
func ValidateImmutableMountDescriptorContract(descriptor ImmutableMountDescriptor, rootDir string) error {
	if strings.TrimSpace(rootDir) != rootDir {
		return fmt.Errorf("runtime root must use its canonical representation")
	}
	if err := validateCanonicalAbsolutePath("runtime root", rootDir); err != nil {
		return err
	}
	if descriptor.EffectiveRoot != rootDir {
		return fmt.Errorf("immutable mount effective root %q differs from runtime root %q", descriptor.EffectiveRoot, rootDir)
	}
	if err := validateCanonicalAbsolutePath("immutable mount effective root", descriptor.EffectiveRoot); err != nil {
		return err
	}
	if !descriptor.Readonly || strings.TrimSpace(descriptor.Identity) == "" || strings.TrimSpace(descriptor.Filesystem) == "" || len(descriptor.LowerDirs) == 0 {
		return fmt.Errorf("rootfs source must provide a complete immutable mount descriptor")
	}
	if !immutableIdentityPattern.MatchString(descriptor.Identity) {
		return fmt.Errorf("immutable mount identity must be a sha256 digest")
	}
	if !immutableFilesystemPattern.MatchString(descriptor.Filesystem) {
		return fmt.Errorf("immutable mount filesystem is malformed: %q", descriptor.Filesystem)
	}
	if len(descriptor.BackingFilesystems) > maxImmutableBackingFilesystems {
		return fmt.Errorf("immutable mount has too many backing filesystems: %d", len(descriptor.BackingFilesystems))
	}
	seenFilesystems := make(map[string]struct{}, len(descriptor.BackingFilesystems))
	previousFilesystem := ""
	for _, filesystem := range descriptor.BackingFilesystems {
		if !immutableFilesystemPattern.MatchString(filesystem) {
			return fmt.Errorf("immutable mount backing filesystem is malformed: %q", filesystem)
		}
		if _, duplicate := seenFilesystems[filesystem]; duplicate {
			return fmt.Errorf("immutable mount backing filesystem is duplicated: %q", filesystem)
		}
		if previousFilesystem != "" && filesystem < previousFilesystem {
			return fmt.Errorf("immutable mount backing filesystems are not canonically ordered")
		}
		seenFilesystems[filesystem] = struct{}{}
		previousFilesystem = filesystem
	}
	if strings.TrimSpace(descriptor.LeaseID) != descriptor.LeaseID || len(descriptor.LeaseID) > maxImmutableLeaseIDBytes || strings.ContainsAny(descriptor.LeaseID, "\x00\n\r\t") {
		return fmt.Errorf("rootfs lease is malformed")
	}
	if len(descriptor.LowerDirs) > maxImmutableLowerDirs {
		return fmt.Errorf("immutable mount has too many lower dirs: %d", len(descriptor.LowerDirs))
	}
	seenLowers := make(map[string]struct{}, len(descriptor.LowerDirs))
	for _, lowerDir := range descriptor.LowerDirs {
		if _, duplicate := seenLowers[lowerDir]; duplicate {
			return fmt.Errorf("immutable lower dir is duplicated: %s", lowerDir)
		}
		seenLowers[lowerDir] = struct{}{}
		if err := validateOverlayPath("immutable lower dir", lowerDir); err != nil {
			return err
		}
	}
	return nil
}

func validateImmutableDirectory(name, candidate string) error {
	if len(candidate) > maxImmutablePathBytes {
		return fmt.Errorf("%s exceeds %d bytes", name, maxImmutablePathBytes)
	}
	if err := validateCanonicalAbsolutePath(name, candidate); err != nil {
		return err
	}
	info, err := os.Lstat(candidate)
	if err != nil {
		return fmt.Errorf("lstat %s %s: %w", name, candidate, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s must not be a symlink: %s", name, candidate)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory: %s", name, candidate)
	}
	return nil
}

func (p *overlayProvider) ReconcilePersistentViews(_ context.Context, runtimeName string, retained map[string]struct{}) error {
	if p.filestoreDir == "" {
		return nil
	}
	var result error
	for _, class := range []string{projectionViewDir, runcViewDir} {
		classRoot := filepath.Join(p.filestoreDir, class)
		entries, err := os.ReadDir(classRoot)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			result = errors.Join(result, fmt.Errorf("read %s views: %w", class, err))
			continue
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			root := filepath.Join(classRoot, entry.Name())
			manifest, err := readProjectionManifest(root)
			if err != nil {
				result = errors.Join(result, fmt.Errorf("retain unowned projection %s: %w", root, err))
				continue
			}
			if manifest.RuntimeName != runtimeName {
				continue
			}
			if _, ok := retained[entry.Name()]; ok {
				// The rootfs source owner and its lease reconcile lower health.
				// Projection owns only the active overlay and its writable state.
				continue
			}
			if err := cleanupPersistedOverlayView(root, p.filestoreDir); err != nil {
				result = errors.Join(result, fmt.Errorf("cleanup stale projection %s: %w", root, err))
			}
		}
	}
	return result
}

func cleanupOverlayView(rootfsRoot string) error {
	return cleanupOverlayViewWithProject(rootfsRoot, "", 0)
}

func cleanupPersistedOverlayView(rootfsRoot, filestoreDir string) error {
	manifest, err := readProjectionManifest(rootfsRoot)
	if os.IsNotExist(err) {
		return cleanupOverlayView(rootfsRoot)
	}
	if err != nil {
		return fmt.Errorf("read projection manifest before cleanup: %w", err)
	}
	return cleanupOverlayViewWithProject(rootfsRoot, filestoreDir, manifest.ProjectID)
}

func cleanupOverlayViewWithProject(rootfsRoot, filestoreDir string, projectID uint32) error {
	if err := unmountOverlayView(overlayView{MergedDir: filepath.Join(rootfsRoot, "merged")}); err != nil {
		return err
	}
	if projectID != 0 {
		if err := clearProjectQuota(filestoreDir, filepath.Join(rootfsRoot, "upper"), projectID); err != nil {
			return err
		}
	}
	return os.RemoveAll(rootfsRoot)
}

func overlayViewForContainer(containerID, filestoreDir, class string, lowerDirs []string) overlayView {
	rootfsRoot := filepath.Join(filestoreDir, class, containerID)
	return overlayView{
		LowerDirs: append([]string(nil), lowerDirs...),
		UpperDir:  filepath.Join(rootfsRoot, "upper"),
		WorkDir:   filepath.Join(rootfsRoot, "work"),
		MergedDir: filepath.Join(rootfsRoot, "merged"),
	}
}

type projectionManifest struct {
	RuntimeName    string                   `json:"runtime_name"`
	RootReadonly   bool                     `json:"root_readonly"`
	HostWritable   bool                     `json:"host_writable"`
	ImmutableMount ImmutableMountDescriptor `json:"immutable_mount"`
	ProjectID      uint32                   `json:"project_id,omitempty"`
}

func writeProjectionManifest(root string, request Request) error {
	content, err := json.Marshal(projectionManifest{
		RuntimeName: request.RuntimeName, RootReadonly: request.Readonly,
		HostWritable: request.NeedsHostWritableRootfs, ImmutableMount: request.ImmutableMount,
		ProjectID: request.ProjectID,
	})
	if err != nil {
		return fmt.Errorf("marshal projection manifest: %w", err)
	}
	return atomicWrite(filepath.Join(root, "projection.json"), content, 0644)
}

func readProjectionManifest(root string) (projectionManifest, error) {
	manifestPath := filepath.Join(root, "projection.json")
	file, err := os.Open(manifestPath)
	if err != nil {
		return projectionManifest{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return projectionManifest{}, err
	}
	if info.Size() <= 0 || info.Size() > maxProjectionManifestBytes {
		return projectionManifest{}, fmt.Errorf("projection manifest size %d is outside the accepted range", info.Size())
	}
	data, err := io.ReadAll(io.LimitReader(file, maxProjectionManifestBytes+1))
	if err != nil {
		return projectionManifest{}, err
	}
	if len(data) > maxProjectionManifestBytes {
		return projectionManifest{}, fmt.Errorf("projection manifest exceeds %d bytes", maxProjectionManifestBytes)
	}
	var manifest projectionManifest
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return projectionManifest{}, err
	}
	if err := ensureJSONEOF(decoder); err != nil {
		return projectionManifest{}, err
	}
	if !immutableFilesystemPattern.MatchString(manifest.RuntimeName) || manifest.ImmutableMount.EffectiveRoot == "" || manifest.ImmutableMount.Identity == "" {
		return projectionManifest{}, fmt.Errorf("projection manifest is incomplete")
	}
	if err := ValidateImmutableMountDescriptorContract(manifest.ImmutableMount, manifest.ImmutableMount.EffectiveRoot); err != nil {
		return projectionManifest{}, fmt.Errorf("projection manifest immutable mount is invalid: %w", err)
	}
	return manifest, nil
}

func ensureJSONEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("projection manifest contains trailing JSON")
		}
		return err
	}
	return nil
}

type PersistentViewExpectation struct {
	RuntimeName string
	ProjectID   uint32
	LimitBytes  int64
}

// VerifyPersistentView proves that an active runc writable root still has the
// projection, kernel project assignment, and hard quota established at create.
func VerifyPersistentView(filestoreDir, containerID string, expected PersistentViewExpectation) error {
	root := filepath.Join(filestoreDir, runcViewDir, containerID)
	manifest, err := readProjectionManifest(root)
	if err != nil {
		return fmt.Errorf("read active rootfs projection: %w", err)
	}
	if manifest.RuntimeName != expected.RuntimeName || !manifest.HostWritable || manifest.ProjectID == 0 || manifest.ProjectID != expected.ProjectID {
		return fmt.Errorf("active rootfs projection enforcement manifest is inconsistent")
	}
	if err := verifyMountedOverlay(filepath.Join(root, "merged")); err != nil {
		return fmt.Errorf("verify active rootfs projection mount: %w", err)
	}
	if err := VerifyProjectQuota(filestoreDir, filepath.Join(root, "upper"), expected.ProjectID, expected.LimitBytes); err != nil {
		return fmt.Errorf("verify active rootfs project quota: %w", err)
	}
	return nil
}

func atomicWrite(target string, content []byte, mode os.FileMode) error {
	return durablefile.Write(target, content, mode)
}

func initializeOverlayView(rootfs overlayView) error {
	return initializeOverlayViewWithMkdir(rootfs, os.Mkdir)
}

func initializeOverlayViewWithMkdir(rootfs overlayView, mkdir func(string, os.FileMode) error) (result error) {
	rootfsRoot := filepath.Dir(rootfs.MergedDir)
	if err := os.MkdirAll(filepath.Dir(rootfsRoot), 0755); err != nil {
		return fmt.Errorf("create writable rootfs view class: %w", err)
	}
	if err := mkdir(rootfsRoot, 0755); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("rootfs view already exists for container at %s", rootfsRoot)
		}
		return fmt.Errorf("create writable rootfs view: %w", err)
	}
	defer func() {
		if result != nil {
			result = errors.Join(result, os.RemoveAll(rootfsRoot))
		}
	}()
	for _, dir := range []string{rootfs.UpperDir, rootfs.WorkDir, rootfs.MergedDir} {
		if err := mkdir(dir, 0755); err != nil {
			return fmt.Errorf("mkdir writable rootfs view dir %s: %w", dir, err)
		}
	}
	return nil
}
