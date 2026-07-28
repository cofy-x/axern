package imageprocess

import (
	"crypto/sha256"
	"encoding/hex"
	"maps"
	"strings"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/workloadidentity"
)

func ValidSpec(spec *runtime.ImageProcessSpec) bool {
	return spec != nil && strings.TrimSpace(spec.GetImage()) != "" && len(spec.GetCommand()) > 0
}

func RuntimeTemplate(runtimeName, image string) *runtime.RuntimeTemplate {
	sum := sha256.Sum256([]byte(runtimeName + "\x00" + image))
	return &runtime.RuntimeTemplate{
		ID:      "image-process-" + hex.EncodeToString(sum[:12]),
		Sandbox: runtimeName,
		Rootfs: &runtime.RootfsConfig{
			Type:     runtime.RootfsSrcType_IMAGE,
			Readonly: false,
			Source:   &runtime.RootfsConfig_ImageUrl{ImageUrl: image},
		},
		Command: append([]string(nil), idleCommand...),
	}
}

func Labels(runtimeID, parentID, image string) map[string]string {
	return map[string]string{
		workloadidentity.LabelKeyRuntimeID: runtimeID,
		KindLabel:                          Kind,
		ParentAllocationLabel:              parentID,
		ImageLabel:                         image,
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
