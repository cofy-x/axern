package startplan

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

func ApplyResolvedSecretEnv(request *runtime.StartRequest, extraConfig ExtraConfig) {
	if request == nil || len(extraConfig.SecretEnv) == 0 {
		return
	}
	if request.UserEnvs == nil {
		request.UserEnvs = map[string]string{}
	}
	for _, item := range extraConfig.SecretEnv {
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		request.UserEnvs[strings.TrimSpace(item.Name)] = item.Value
	}
}

func MaterializeResolvedSecretFiles(request *runtime.StartRequest, extraConfig ExtraConfig) (func(), error) {
	if request == nil || len(extraConfig.SecretFiles) == 0 {
		return func() {}, nil
	}
	secretRoot := filepath.Join(os.TempDir(), "axnoded-secrets", request.GetContainerID())
	if err := os.RemoveAll(secretRoot); err != nil {
		return nil, fmt.Errorf("cleanup previous secret root: %w", err)
	}
	for _, item := range extraConfig.SecretFiles {
		target := strings.TrimSpace(item.Path)
		if target == "" {
			continue
		}
		rel := strings.TrimPrefix(target, "/")
		hostPath := filepath.Join(secretRoot, rel)
		if err := os.MkdirAll(filepath.Dir(hostPath), 0o700); err != nil {
			return nil, fmt.Errorf("create secret dir: %w", err)
		}
		content, err := base64.StdEncoding.DecodeString(item.Content)
		if err != nil {
			return nil, fmt.Errorf("decode secret file content for %s: %w", target, err)
		}
		mode := os.FileMode(item.Mode)
		if mode == 0 {
			mode = 0o400
		}
		if err := os.WriteFile(hostPath, content, mode); err != nil {
			return nil, fmt.Errorf("write secret file %s: %w", target, err)
		}
		request.Mounts = append(request.Mounts, &runtime.Mount{
			Type:    "bind",
			Source:  hostPath,
			Target:  target,
			Options: []string{"ro"},
		})
	}
	return func() {
		_ = os.RemoveAll(secretRoot)
	}, nil
}
