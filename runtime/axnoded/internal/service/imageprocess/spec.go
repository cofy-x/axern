package imageprocess

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"strings"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

func ValidSpec(spec *runtime.ImageProcessSpec) bool {
	return spec != nil && strings.TrimSpace(spec.GetImage()) != "" && len(spec.GetCommand()) > 0
}

func RuntimeTemplate(image string) *runtime.RuntimeTemplate {
	sum := sha256.Sum256([]byte(image))
	return &runtime.RuntimeTemplate{
		ID: "image-process-" + hex.EncodeToString(sum[:12]),
		Rootfs: &runtime.RootfsConfig{
			Type:     runtime.RootfsSrcType_IMAGE,
			Readonly: false,
			Source:   &runtime.RootfsConfig_ImageUrl{ImageUrl: image},
		},
		Command: append([]string(nil), idleCommand...),
	}
}

func Labels(parentID, image string) map[string]string {
	return map[string]string{
		KindLabel:             Kind,
		ParentAllocationLabel: parentID,
		ImageLabel:            image,
	}
}

func Open(containerID string, spec *runtime.ImageProcessSpec) *runtime.ProcessOpen {
	return &runtime.ProcessOpen{
		ID:           containerID,
		Command:      append([]string(nil), spec.GetCommand()...),
		Tty:          spec.GetTty(),
		Timeout:      spec.GetTimeout(),
		Env:          cloneEnv(spec.GetEnv()),
		Cwd:          spec.GetCwd(),
		User:         spec.GetUser(),
		ManagedProxy: spec.GetManagedProxy(),
	}
}

func cloneEnv(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
