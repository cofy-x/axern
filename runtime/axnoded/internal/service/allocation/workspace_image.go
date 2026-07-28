package allocation

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	langrtmanager "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/startplan"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

const workspaceViewsDir = "workspace-views"

type workspaceImageRecord struct {
	payloadRoot string
	taskRoot    string
	merged      string
	target      string
	preparation *commonv1.WorkspacePreparationFacts
	cleanup     func()
}

func (h *Controller) resolveWorkspaceImage(request *runtime.StartRequest, extraConfig startplan.ExtraConfig) (*runtime.Mount, func(), error) {
	workspace := request.GetWorkspaceImage()
	if workspace == nil {
		return nil, func() {}, nil
	}
	if h == nil || h.lrtManager == nil {
		return nil, nil, fmt.Errorf("workspace image runtime manager is unavailable: %w", errord.ErrFailedPrecondition)
	}
	if err := validateWorkspaceImage(workspace); err != nil {
		return nil, nil, err
	}
	var failures []string
	for _, variant := range workspace.GetVariants() {
		variantStarted := time.Now()
		cfg := langrtmanager.RootfsConfig{SrcType: runtime.RootfsSrcType_IMAGE, ImageUrl: strings.TrimSpace(variant.GetImage()), DockerConfigJSON: strings.TrimSpace(extraConfig.DockerConfigJSON)}
		resolveStarted := time.Now()
		resolved, err := h.lrtManager.ResolveRootfsConfig(cfg)
		resolveDuration := time.Since(resolveStarted)
		if err != nil {
			failures = append(failures, variant.GetFormat()+": "+err.Error())
			continue
		}
		rootfs, rootfsReport, err := h.lrtManager.GetRootfsWithReport(resolved)
		if err != nil {
			failures = append(failures, variant.GetFormat()+": "+err.Error())
			continue
		}
		if err := rootfs.IncActiveRef(); err != nil {
			failures = append(failures, variant.GetFormat()+": "+err.Error())
			continue
		}
		lower, err := workspaceLowerPath(rootfs.Path(), workspace.GetSourcePath())
		if err != nil {
			rootfs.ReleaseActiveRef()
			return nil, nil, err
		}
		cowStarted := time.Now()
		merged, overlayCleanup, err := prepareWorkspaceCOW(h.config.RuntimeConfig.FilestoreDir, request.GetContainerID(), lower)
		if err != nil {
			rootfs.ReleaseActiveRef()
			return nil, nil, err
		}
		cowDuration := time.Since(cowStarted)
		cleanup := func() { overlayCleanup(); rootfs.ReleaseActiveRef() }
		preparation := &commonv1.WorkspacePreparationFacts{
			PayloadFormat:  strings.ToLower(strings.TrimSpace(variant.GetFormat())),
			PayloadDigest:  imageDigest(variant.GetImage()),
			CacheHit:       rootfsReportCacheHit(rootfsReport),
			ImageResolveMs: resolveDuration.Milliseconds(),
			ImagePullMs:    rootfsReportPullDuration(rootfsReport).Milliseconds(),
			CowPrepareMs:   cowDuration.Milliseconds(),
		}
		h.rememberWorkspaceImage(request.GetContainerID(), workspaceImageRecord{
			payloadRoot: rootfs.Path(),
			taskRoot:    strings.TrimSuffix(path.Clean(workspace.GetSourcePath()), "/workspace"),
			merged:      merged,
			target:      workspaceTarget(workspace.GetTarget()),
			preparation: preparation,
			cleanup:     cleanup,
		})
		if err := h.rememberWorkspaceImageSpec(request.GetContainerID(), variant.GetImage(), workspace.GetSourcePath(), workspaceTarget(workspace.GetTarget())); err != nil {
			h.forgetWorkspaceImage(request.GetContainerID())
			return nil, nil, fmt.Errorf("persist workspace image ownership: %w", err)
		}
		logrus.WithFields(logrus.Fields{
			"container_id":   request.GetContainerID(),
			"format":         variant.GetFormat(),
			"image":          variant.GetImage(),
			"digest":         imageDigest(variant.GetImage()),
			"source_path":    workspace.GetSourcePath(),
			"cache_hit":      preparation.GetCacheHit(),
			"cow_prepare_ms": preparation.GetCowPrepareMs(),
			"workspace_ms":   time.Since(variantStarted).Milliseconds(),
			"fallbacks":      append([]string(nil), failures...),
		}).Info("prepared copy-on-write workspace image")
		return &runtime.Mount{Type: "bind", Source: merged, Target: workspaceTarget(workspace.GetTarget()), Options: []string{"rbind", "rw"}}, func() { h.forgetWorkspaceImage(request.GetContainerID()) }, nil
	}
	return nil, nil, fmt.Errorf("no workspace image variant could be resolved (%s): %w", strings.Join(failures, "; "), errord.ErrFailedPrecondition)
}

func rootfsReportCacheHit(report langrtmanager.RootfsPrepareReport) bool {
	for _, sample := range report.Steps {
		if sample.Step != contract.StartupStepRootfsCacheLookup && sample.Step != contract.StartupStepRootfsWait {
			return false
		}
	}
	return true
}

func rootfsReportPullDuration(report langrtmanager.RootfsPrepareReport) time.Duration {
	var total time.Duration
	for _, sample := range report.Steps {
		if sample.Step == contract.StartupStepRootfsMount {
			total += sample.Duration
		}
	}
	return total
}

func imageDigest(reference string) string {
	_, digest, ok := strings.Cut(reference, "@")
	if !ok {
		return ""
	}
	return digest
}

func validateWorkspaceImage(workspace *runtime.WorkspaceImageSource) error {
	if len(workspace.GetVariants()) == 0 {
		return fmt.Errorf("workspace image variants are required: %w", errord.ErrInvalidArgument)
	}
	seenFormats := map[string]bool{}
	for _, variant := range workspace.GetVariants() {
		if variant == nil {
			return fmt.Errorf("workspace variant must not be nil: %w", errord.ErrInvalidArgument)
		}
		format := strings.ToLower(strings.TrimSpace(variant.GetFormat()))
		if format != "nydus" && format != "oci" {
			return fmt.Errorf("workspace variant format %q is unsupported: %w", format, errord.ErrInvalidArgument)
		}
		if strings.TrimSpace(variant.GetImage()) == "" {
			return fmt.Errorf("workspace variant image is required: %w", errord.ErrInvalidArgument)
		}
		if seenFormats[format] {
			return fmt.Errorf("workspace variant format %q is duplicated: %w", format, errord.ErrInvalidArgument)
		}
		seenFormats[format] = true
		if !immutableWorkspaceImageReference(variant.GetImage()) {
			return fmt.Errorf("workspace variant image must use an immutable sha256 digest reference: %w", errord.ErrInvalidArgument)
		}
	}
	source := path.Clean(strings.TrimSpace(workspace.GetSourcePath()))
	sourceParts := strings.Split(source, "/")
	if len(sourceParts) != 3 || sourceParts[0] != "tasks" || sourceParts[1] == "" || sourceParts[2] != "workspace" || pathHasParentReference(workspace.GetSourcePath()) {
		return fmt.Errorf("workspace source_path %q must select tasks/<id>/workspace: %w", workspace.GetSourcePath(), errord.ErrInvalidArgument)
	}
	target := workspaceTarget(workspace.GetTarget())
	if target == "/" || !strings.HasPrefix(target, "/") || pathHasParentReference(workspace.GetTarget()) {
		return fmt.Errorf("workspace target %q is invalid: %w", workspace.GetTarget(), errord.ErrInvalidArgument)
	}
	if protectedWorkspaceTarget(target) {
		return fmt.Errorf("workspace target %q is protected: %w", target, errord.ErrInvalidArgument)
	}
	return nil
}

func immutableWorkspaceImageReference(value string) bool {
	_, digest, ok := strings.Cut(strings.TrimSpace(value), "@sha256:")
	if !ok || len(digest) != 64 {
		return false
	}
	for _, r := range digest {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func protectedWorkspaceTarget(target string) bool {
	for _, protected := range []string{"/bin", "/dev", "/etc", "/lib", "/lib64", "/mnt", "/proc", "/run", "/sbin", "/sys", "/usr"} {
		if target == protected || strings.HasPrefix(target, protected+"/") {
			return true
		}
	}
	return false
}

func workspaceLowerPath(root, source string) (string, error) {
	lower := filepath.Join(root, filepath.FromSlash(path.Clean(source)))
	rel, err := filepath.Rel(root, lower)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace source escapes payload root: %w", errord.ErrInvalidArgument)
	}
	if err := rejectSymlinkComponents(root, lower); err != nil {
		return "", err
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve payload root: %w", err)
	}
	resolvedLower, err := filepath.EvalSymlinks(lower)
	if err != nil {
		return "", fmt.Errorf("workspace source %q: %w", source, err)
	}
	resolvedRel, err := filepath.Rel(resolvedRoot, resolvedLower)
	if err != nil || resolvedRel == ".." || strings.HasPrefix(resolvedRel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("workspace source resolves outside payload root: %w", errord.ErrInvalidArgument)
	}
	info, err := os.Stat(resolvedLower)
	if err != nil {
		return "", fmt.Errorf("workspace source %q: %w", source, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workspace source %q is not a directory: %w", source, errord.ErrInvalidArgument)
	}
	return resolvedLower, nil
}

func workspaceTarget(target string) string {
	target = path.Clean(strings.TrimSpace(target))
	if target == "." {
		return "/workspace"
	}
	return target
}

func (h *Controller) WorkspacePreparation(containerID string) *commonv1.WorkspacePreparationFacts {
	if h == nil {
		return nil
	}
	h.stateMu.RLock()
	state := h.allocationStates[containerID]
	var preparation *commonv1.WorkspacePreparationFacts
	if state != nil {
		preparation = state.workspace.preparation
	}
	h.stateMu.RUnlock()
	if preparation == nil {
		return nil
	}
	return proto.Clone(preparation).(*commonv1.WorkspacePreparationFacts)
}

func (h *Controller) MaterializeTaskAssets(containerID, sourcePath, target string, kind runtime.TaskAssetKind) (int64, error) {
	started := time.Now()
	unlockLifecycle := h.allocationLifecycleLocks.Lock(containerID)
	defer unlockLifecycle()
	h.stateMu.RLock()
	state := h.allocationStates[containerID]
	var record workspaceImageRecord
	if state != nil {
		record = state.workspace
	}
	h.stateMu.RUnlock()
	if state == nil {
		return 0, fmt.Errorf("allocation has no TaskSet workspace image: %w", errord.ErrFailedPrecondition)
	}
	prefix := record.taskRoot + "/verifier"
	if kind == runtime.TaskAssetKind_TASK_ASSET_KIND_ORACLE {
		prefix = record.taskRoot + "/oracle"
	} else if kind != runtime.TaskAssetKind_TASK_ASSET_KIND_VERIFIER {
		return 0, fmt.Errorf("task asset kind is required: %w", errord.ErrInvalidArgument)
	}
	sourcePath = path.Clean(strings.TrimSpace(sourcePath))
	if (sourcePath != prefix && !strings.HasPrefix(sourcePath, prefix+"/")) || pathHasParentReference(sourcePath) {
		return 0, fmt.Errorf("task asset source %q is outside %s: %w", sourcePath, prefix, errord.ErrInvalidArgument)
	}
	target = path.Clean(strings.TrimSpace(target))
	if (target != record.target && !strings.HasPrefix(target, record.target+"/")) || pathHasParentReference(target) {
		return 0, fmt.Errorf("task asset target %q is outside workspace %s: %w", target, record.target, errord.ErrInvalidArgument)
	}
	source := filepath.Join(record.payloadRoot, filepath.FromSlash(sourcePath))
	if err := rejectSymlinkComponents(record.payloadRoot, source); err != nil {
		return 0, err
	}
	relTarget := strings.TrimPrefix(strings.TrimPrefix(target, record.target), "/")
	destination := filepath.Join(record.merged, filepath.FromSlash(relTarget))
	if err := rejectDestinationSymlinkComponents(record.merged, destination); err != nil {
		return 0, err
	}
	if err := copyTaskAsset(source, record.merged, relTarget); err != nil {
		return 0, err
	}
	return time.Since(started).Milliseconds(), nil
}

func rejectDestinationSymlinkComponents(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("task asset target escapes workspace: %w", errord.ErrInvalidArgument)
	}
	current := filepath.Clean(root)
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if os.IsNotExist(statErr) {
			continue
		}
		if statErr != nil {
			return statErr
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("task asset target contains symlink at %s: %w", current, errord.ErrInvalidArgument)
		}
	}
	return nil
}

func rejectSymlinkComponents(root, target string) error {
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("payload path escapes root: %w", errord.ErrInvalidArgument)
	}
	current := filepath.Clean(root)
	for _, component := range strings.Split(rel, string(filepath.Separator)) {
		if component == "." || component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("payload path contains symlink at %s: %w", current, errord.ErrInvalidArgument)
		}
	}
	return nil
}
