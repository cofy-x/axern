package publicv1

import (
	"path"
	"regexp"
	"strings"

	executionkernel "github.com/cofy-x/axern/control/controld/internal/kernel/execution"
	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/google/go-containerregistry/pkg/name"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

var secretEnvironmentNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

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
		if !secretEnvironmentNamePattern.MatchString(name) {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.secret_env name %q is invalid", name)
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
		rawPath := strings.TrimSpace(item.GetPath())
		if rawPath == "" {
			return grpcstatus.Error(codes.InvalidArgument, "config.secret_files.path is required")
		}
		cleanPath := path.Clean(rawPath)
		if cleanPath == "/" || !strings.HasPrefix(cleanPath, "/") || pathHasParentReference(rawPath) {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.secret_files path %q must be an absolute container path below /", rawPath)
		}
		if protectedSecretFileTarget(cleanPath) {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.secret_files path %q overlaps a protected runtime path", cleanPath)
		}
		for existing := range seenFiles {
			if pathsOverlap(existing, cleanPath) {
				return grpcstatus.Errorf(codes.InvalidArgument, "config.secret_files path %q overlaps path %q", cleanPath, existing)
			}
		}
		seenFiles[cleanPath] = struct{}{}
		if strings.TrimSpace(item.GetSecretID()) == "" {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.secret_files %q secret_id is required", cleanPath)
		}
		if strings.TrimSpace(item.GetKey()) == "" {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.secret_files %q key is required", cleanPath)
		}
		if item.GetMode() > 0o777 || item.GetMode()&0o222 != 0 {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.secret_files %q mode must be read-only permissions within 0777", cleanPath)
		}
	}
	return nil
}

func protectedSecretFileTarget(target string) bool {
	for _, protected := range []string{"/bin", "/boot", "/dev", "/lib", "/lib64", "/proc", "/sbin", "/sys", "/usr", "/run/axern", "/var/run/axern"} {
		if pathsOverlap(target, protected) {
			return true
		}
	}
	for _, protected := range []string{"/etc/group", "/etc/gshadow", "/etc/hostname", "/etc/hosts", "/etc/ld.so.preload", "/etc/passwd", "/etc/resolv.conf", "/etc/shadow"} {
		if pathsOverlap(target, protected) {
			return true
		}
	}
	return false
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

func validateExecutionConfigNetwork(config *commonv1.ExecutionConfig) error {
	if config == nil {
		return nil
	}
	return executionkernel.ValidateNetwork(config.GetNetwork())
}

func validateExecutionConfigCapabilities(config *commonv1.ExecutionConfig) error {
	if config == nil {
		return nil
	}
	if err := capabilitycontract.ValidateExtensionRequirements(config.GetExtensionCapabilityRequirements()); err != nil {
		return grpcstatus.Errorf(codes.InvalidArgument, "config.extension_capability_requirements: %v", err)
	}
	return nil
}

func validateExecutionConfigImageMounts(config *commonv1.ExecutionConfig) error {
	if config == nil {
		return nil
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
		if _, err := name.ParseReference(image, name.WeakValidation); err != nil {
			return grpcstatus.Errorf(codes.InvalidArgument, "config.image_mounts image %q is not a valid OCI reference", image)
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
	if err := validateImageMountNoSecretFileOverlap(config); err != nil {
		return err
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
	for _, protected := range []string{"/bin", "/dev", "/etc", "/lib", "/lib64", "/mnt", "/proc", "/run", "/sbin", "/sys", "/usr"} {
		if pathsOverlap(target, protected) {
			return true
		}
	}
	return false
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
