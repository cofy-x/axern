package rootfsview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"

	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/sirupsen/logrus"
)

const (
	projectionViewDir = "projections"
	runcViewDir       = "runc"
)

var mountInfoOctalEscapePattern = regexp.MustCompile(`\\[0-7]{3}`)

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
	MountID    int      `json:"mount_id"`
	Mountpoint string   `json:"mountpoint"`
	MountRoot  string   `json:"mount_root"`
	FSType     string   `json:"fs_type"`
	Source     string   `json:"source"`
	Readonly   bool     `json:"readonly"`
	LowerDirs  []string `json:"lower_dirs"`
}

type Request struct {
	RootDir                 string
	Readonly                bool
	RuntimeName             string
	NeedsHostWritableRootfs bool
	Backing                 RootfsBackingFacts
	Targets                 []MountTarget
	WritableLayerLimitBytes int64
	ProjectID               uint32
	RootfsLeaseID           string
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

	backing := request.Backing
	if backing.Mountpoint == "" {
		backing, err = InspectBacking(request.RootDir)
		if err != nil {
			return View{}, err
		}
	}
	lowerDirs := backing.LowerDirs
	if len(lowerDirs) == 0 {
		lowerDirs = []string{request.RootDir}
	}
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
		if err := applyProjectQuota(p.filestoreDir, view.UpperDir, request.ProjectID, request.WritableLayerLimitBytes); err != nil {
			metrics.RecordWritableLayerOperation(request.RuntimeName, "project_quota", "failure")
			_ = cleanupOverlayView(filepath.Dir(view.MergedDir))
			return View{}, err
		}
		metrics.RecordWritableLayerOperation(request.RuntimeName, "project_quota", "success")
	}
	if err := mountOverlayView(view); err != nil {
		result := "failure"
		if errors.Is(err, syscall.ENOSPC) {
			result = "enospc"
		}
		metrics.RecordWritableLayerOperation(request.RuntimeName, "projection_mount", result)
		_ = cleanupOverlayView(filepath.Dir(view.MergedDir))
		return View{}, err
	}
	metrics.RecordWritableLayerOperation(request.RuntimeName, "projection_mount", "success")
	if err := writeProjectionManifest(filepath.Dir(view.MergedDir), request, backing); err != nil {
		_ = cleanupOverlayView(filepath.Dir(view.MergedDir))
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
		"backing_fs":      backing.FSType,
	}).Debug("prepared sandbox-private rootfs view")
	return View{RootDir: view.MergedDir, Prepared: true}, nil
}

func inspectMountTargets(root string, targets []MountTarget) ([]MountTarget, error) {
	missing := make([]MountTarget, 0)
	for _, target := range targets {
		destination := path.Clean(strings.TrimSpace(target.Destination))
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
			target.Destination = destination
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
		if err := cleanupOverlayView(filepath.Join(p.filestoreDir, class, containerID)); err != nil {
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
				if err := validateProjectionBacking(manifest.Backing); err != nil {
					result = errors.Join(result, fmt.Errorf("active projection %s is degraded: %w", root, err))
				}
				continue
			}
			if err := cleanupOverlayView(root); err != nil {
				result = errors.Join(result, fmt.Errorf("cleanup stale projection %s: %w", root, err))
			}
		}
	}
	return result
}

func cleanupOverlayView(rootfsRoot string) error {
	if err := unmountOverlayView(overlayView{MergedDir: filepath.Join(rootfsRoot, "merged")}); err != nil {
		return err
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
	RuntimeName   string             `json:"runtime_name"`
	RootReadonly  bool               `json:"root_readonly"`
	HostWritable  bool               `json:"host_writable"`
	Backing       RootfsBackingFacts `json:"backing"`
	RootfsLeaseID string             `json:"rootfs_lease_id,omitempty"`
}

func writeProjectionManifest(root string, request Request, backing RootfsBackingFacts) error {
	content, err := json.Marshal(projectionManifest{request.RuntimeName, request.Readonly, request.NeedsHostWritableRootfs, backing, request.RootfsLeaseID})
	if err != nil {
		return fmt.Errorf("marshal projection manifest: %w", err)
	}
	return atomicWrite(filepath.Join(root, "projection.json"), content, 0644)
}

func readProjectionManifest(root string) (projectionManifest, error) {
	data, err := os.ReadFile(filepath.Join(root, "projection.json"))
	if err != nil {
		return projectionManifest{}, err
	}
	var manifest projectionManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return projectionManifest{}, err
	}
	if manifest.RuntimeName == "" || manifest.Backing.Mountpoint == "" || manifest.Backing.MountID == 0 {
		return projectionManifest{}, fmt.Errorf("projection manifest is incomplete")
	}
	return manifest, nil
}

func validateProjectionBacking(expected RootfsBackingFacts) error {
	actual, err := InspectBacking(expected.Mountpoint)
	if err != nil {
		return err
	}
	if actual.MountID != expected.MountID || actual.FSType != expected.FSType || actual.Source != expected.Source || actual.MountRoot != expected.MountRoot {
		return fmt.Errorf("lower mount identity changed: expected id=%d fs=%s root=%s source=%s, got id=%d fs=%s root=%s source=%s", expected.MountID, expected.FSType, expected.MountRoot, expected.Source, actual.MountID, actual.FSType, actual.MountRoot, actual.Source)
	}
	return nil
}

func atomicWrite(target string, content []byte, mode os.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(target), ".projection-")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryPath)
	}()
	if err := temporary.Chmod(mode); err != nil {
		return err
	}
	if _, err := temporary.Write(content); err != nil {
		return err
	}
	if err := temporary.Sync(); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(target))
	if err != nil {
		return err
	}
	defer directory.Close()
	return directory.Sync()
}

func initializeOverlayView(rootfs overlayView) error {
	rootfsRoot := filepath.Dir(rootfs.MergedDir)
	if err := os.MkdirAll(filepath.Dir(rootfsRoot), 0755); err != nil {
		return fmt.Errorf("create writable rootfs view class: %w", err)
	}
	if err := os.Mkdir(rootfsRoot, 0755); err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("rootfs view already exists for container at %s", rootfsRoot)
		}
		return fmt.Errorf("create writable rootfs view: %w", err)
	}
	for _, dir := range []string{rootfs.UpperDir, rootfs.WorkDir, rootfs.MergedDir} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("mkdir writable rootfs view dir %s: %w", dir, err)
		}
	}
	return nil
}
