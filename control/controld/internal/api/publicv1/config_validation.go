package publicv1

import (
	"path"
	"strings"

	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func validateExecutionConfigSecretRefs(config *commonv1.ExecutionConfig) error {
	if config == nil {
		return nil
	}
	seenEnv := map[string]struct{}{}
	for _, item := range config.GetSecretEnv() {
		if item == nil {
			continue
		}
		name := strings.TrimSpace(item.GetName())
		if name == "" {
			return grpcstatus.Error(codes.InvalidArgument, "config.secret_env.name is required")
		}
		if _, exists := seenEnv[name]; exists {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.secret_env %q is duplicated", name)
		}
		seenEnv[name] = struct{}{}
		if strings.TrimSpace(item.GetSecretID()) == "" {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.secret_env %q secret_id is required", name)
		}
		if strings.TrimSpace(item.GetKey()) == "" {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.secret_env %q key is required", name)
		}
	}

	seenFiles := map[string]struct{}{}
	for _, item := range config.GetSecretFiles() {
		if item == nil {
			continue
		}
		path := strings.TrimSpace(item.GetPath())
		if path == "" {
			return grpcstatus.Error(codes.InvalidArgument, "config.secret_files.path is required")
		}
		if _, exists := seenFiles[path]; exists {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.secret_files %q is duplicated", path)
		}
		seenFiles[path] = struct{}{}
		if strings.TrimSpace(item.GetSecretID()) == "" {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.secret_files %q secret_id is required", path)
		}
		if strings.TrimSpace(item.GetKey()) == "" {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.secret_files %q key is required", path)
		}
	}
	return nil
}

func validateOptionalExecutionArgv(config *commonv1.ExecutionConfig) error {
	if config == nil || len(config.GetArgv()) == 0 {
		return nil
	}
	if strings.TrimSpace(config.GetArgv()[0]) == "" {
		return grpcstatus.Error(codes.InvalidArgument, "config.argv[0] must be non-empty when argv is set")
	}
	return nil
}

func validateExecutionConfigResources(config *commonv1.ExecutionConfig) error {
	if config == nil {
		return nil
	}
	return executionkernel.ValidateResources(config.GetResources())
}

func validateExecutionConfigImageMounts(config *commonv1.ExecutionConfig) error {
	if config == nil {
		return nil
	}
	if err := validateWorkspaceImage(config.GetWorkspaceImage()); err != nil {
		return err
	}
	if workspace := config.GetWorkspaceImage(); workspace != nil {
		workspaceTarget := path.Clean(workspace.GetTarget())
		if workspaceTarget == "." {
			workspaceTarget = "/workspace"
		}
		for _, imageMount := range config.GetImageMounts() {
			if imageMount != nil && pathsOverlap(workspaceTarget, imageMount.GetTarget()) {
				return grpcstatus.Errorf(codes.InvalidArgument, "config.workspace_image target %q overlaps config.image_mounts target %q", workspaceTarget, imageMount.GetTarget())
			}
		}
		for _, volume := range config.GetVolumeMounts() {
			if volume != nil && pathsOverlap(workspaceTarget, volume.GetTarget()) {
				return grpcstatus.Errorf(codes.InvalidArgument, "config.workspace_image target %q overlaps config.volume_mounts target %q", workspaceTarget, volume.GetTarget())
			}
		}
		for _, secretFile := range config.GetSecretFiles() {
			if secretFile != nil && pathsOverlap(workspaceTarget, secretFile.GetPath()) {
				return grpcstatus.Errorf(codes.InvalidArgument, "config.workspace_image target %q overlaps config.secret_files path %q", workspaceTarget, secretFile.GetPath())
			}
		}
	}
	if len(config.GetImageMounts()) == 0 {
		return nil
	}
	seenTargets := map[string]struct{}{}
	for _, item := range config.GetImageMounts() {
		if item == nil {
			continue
		}
		image := strings.TrimSpace(item.GetImage())
		if image == "" {
			return grpcstatus.Error(codes.InvalidArgument, "config.image_mounts.image is required")
		}
		rawTarget := strings.TrimSpace(item.GetTarget())
		target := path.Clean(rawTarget)
		if target == "." || target == "/" || !strings.HasPrefix(target, "/") || pathHasParentReference(rawTarget) {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.image_mounts %q target must be an absolute container path below /", image)
		}
		if protectedImageMountTarget(target) {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.image_mounts target %q is protected", target)
		}
		for existing := range seenTargets {
			if pathsOverlap(existing, target) {
				return grpcstatus.Errorf(codes.InvalidArgument, "config.image_mounts target %q overlaps target %q", target, existing)
			}
		}
		seenTargets[target] = struct{}{}
	}
	if err := validateImageMountNoVolumeOverlap(config); err != nil {
		return err
	}
	if err := validateImageMountNoSecretFileOverlap(config); err != nil {
		return err
	}
	return nil
}

func validateWorkspaceImage(workspace *commonv1.WorkspaceImageSource) error {
	if workspace == nil {
		return nil
	}
	if len(workspace.GetVariants()) == 0 {
		return grpcstatus.Error(codes.InvalidArgument, "config.workspace_image.variants is required")
	}
	seenFormats := map[string]bool{}
	for _, variant := range workspace.GetVariants() {
		if variant == nil {
			return grpcstatus.Error(codes.InvalidArgument, "config.workspace_image variant must not be nil")
		}
		format := strings.ToLower(strings.TrimSpace(variant.GetFormat()))
		if format != "nydus" && format != "oci" {
			return grpcstatus.Error(codes.InvalidArgument, "config.workspace_image variant format must be nydus or oci")
		}
		if strings.TrimSpace(variant.GetImage()) == "" {
			return grpcstatus.Error(codes.InvalidArgument, "config.workspace_image variant image is required")
		}
		if !immutableWorkspaceImageReference(variant.GetImage()) {
			return grpcstatus.Error(codes.InvalidArgument, "config.workspace_image variant image must use an immutable sha256 digest reference")
		}
		if seenFormats[format] {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.workspace_image variant format %q is duplicated", format)
		}
		seenFormats[format] = true
	}
	sourcePath := strings.TrimSpace(workspace.GetSourcePath())
	cleanSource := path.Clean(sourcePath)
	parts := strings.Split(cleanSource, "/")
	if len(parts) != 3 || parts[0] != "tasks" || parts[1] == "" || parts[2] != "workspace" || pathHasParentReference(sourcePath) {
		return grpcstatus.Error(codes.InvalidArgument, "config.workspace_image.source_path must select tasks/<id>/workspace")
	}
	rawTarget := strings.TrimSpace(workspace.GetTarget())
	target := path.Clean(rawTarget)
	if target == "." {
		target = "/workspace"
	}
	if target == "/" || !strings.HasPrefix(target, "/") || pathHasParentReference(rawTarget) {
		return grpcstatus.Error(codes.InvalidArgument, "config.workspace_image.target must be an absolute path below /")
	}
	if protectedImageMountTarget(target) {
		return grpcstatus.Errorf(codes.InvalidArgument, "config.workspace_image target %q is protected", target)
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

func validateNoServiceVolumeMounts(config *commonv1.ExecutionConfig, owner string) error {
	if config == nil || len(config.GetVolumeMounts()) == 0 {
		return nil
	}
	return grpcstatus.Errorf(codes.InvalidArgument, "config.volume_mounts is only supported for service workloads in v1, not %s", owner)
}

func validateServiceVolumeMounts(config *commonv1.ExecutionConfig) error {
	if config == nil {
		return nil
	}
	seenNames := map[string]struct{}{}
	seenTargets := map[string]struct{}{}
	for _, item := range config.GetVolumeMounts() {
		if item == nil {
			continue
		}
		name := strings.TrimSpace(item.GetName())
		if name == "" {
			return grpcstatus.Error(codes.InvalidArgument, "config.volume_mounts.name is required")
		}
		if !isStableVolumeName(name) {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.volume_mounts %q name may only contain letters, digits, '.', '_', or '-'", name)
		}
		if _, exists := seenNames[name]; exists {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.volume_mounts %q is duplicated", name)
		}
		seenNames[name] = struct{}{}

		rawTarget := strings.TrimSpace(item.GetTarget())
		target := path.Clean(rawTarget)
		if target == "." || target == "/" || !strings.HasPrefix(target, "/") || pathHasParentReference(rawTarget) {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.volume_mounts %q target must be an absolute container path below /", name)
		}
		if _, exists := seenTargets[target]; exists {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.volume_mounts target %q is duplicated", target)
		}
		seenTargets[target] = struct{}{}

		for _, option := range item.GetOptions() {
			option = strings.TrimSpace(option)
			if option == "" {
				continue
			}
			if !allowedServiceVolumeOption(option) {
				return grpcstatus.Errorf(codes.InvalidArgument, "config.volume_mounts %q option %q is not supported", name, option)
			}
		}
	}
	return nil
}

func validateImageMountNoVolumeOverlap(config *commonv1.ExecutionConfig) error {
	for _, imageMount := range config.GetImageMounts() {
		if imageMount == nil {
			continue
		}
		imageTarget := path.Clean(strings.TrimSpace(imageMount.GetTarget()))
		for _, volume := range config.GetVolumeMounts() {
			if volume == nil {
				continue
			}
			volumeTarget := path.Clean(strings.TrimSpace(volume.GetTarget()))
			if pathsOverlap(imageTarget, volumeTarget) {
				return grpcstatus.Errorf(codes.InvalidArgument, "config.image_mounts target %q overlaps config.volume_mounts target %q", imageTarget, volumeTarget)
			}
		}
	}
	return nil
}

func validateImageMountNoSecretFileOverlap(config *commonv1.ExecutionConfig) error {
	for _, imageMount := range config.GetImageMounts() {
		if imageMount == nil {
			continue
		}
		imageTarget := path.Clean(strings.TrimSpace(imageMount.GetTarget()))
		for _, secretFile := range config.GetSecretFiles() {
			if secretFile == nil {
				continue
			}
			secretPath := path.Clean(strings.TrimSpace(secretFile.GetPath()))
			if pathsOverlap(imageTarget, secretPath) {
				return grpcstatus.Errorf(codes.InvalidArgument, "config.image_mounts target %q overlaps config.secret_files path %q", imageTarget, secretPath)
			}
		}
	}
	return nil
}

func protectedImageMountTarget(target string) bool {
	switch target {
	case "/bin", "/dev", "/etc", "/lib", "/lib64", "/mnt", "/proc", "/run", "/sbin", "/sys", "/usr":
		return true
	default:
		return false
	}
}

func pathsOverlap(a, b string) bool {
	a = path.Clean(a)
	b = path.Clean(b)
	return a == b || strings.HasPrefix(a, b+"/") || strings.HasPrefix(b, a+"/")
}

func pathHasParentReference(value string) bool {
	for _, part := range strings.Split(value, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

func isStableVolumeName(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}

func allowedServiceVolumeOption(option string) bool {
	switch option {
	case "ro", "rw", "rbind", "nosuid", "nodev", "noexec":
		return true
	default:
		return false
	}
}

func validateServiceReadinessProbe(probe *servicev1.ServiceProbe) error {
	_, err := servicekernel.ValidateAndNormalizeReadinessProbe(probe)
	return err
}

func validateServiceLivenessProbe(probe *servicev1.ServiceProbe) error {
	_, err := servicekernel.ValidateAndNormalizeLivenessProbe(probe)
	return err
}

func validateServiceAutoscalingPolicy(policy *servicev1.ServiceAutoscalingPolicy) error {
	_, err := servicekernel.ValidateAndNormalizeAutoscalingPolicy(policy)
	return err
}
