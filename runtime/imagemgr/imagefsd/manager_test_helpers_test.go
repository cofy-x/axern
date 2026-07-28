package imagefsd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/imagemgr/pkg/registryauth"
)

type mockNydusClient struct {
	fetchAndExtractFunc func(ctx context.Context, imageURL string, outputDir string) (string, error)
	useHTTPForFunc      func(imageURL string) bool
}

func (m *mockNydusClient) FetchAndExtractBootstrap(ctx context.Context, imageURL string, outputDir string) (string, []string, error) {
	if m.fetchAndExtractFunc != nil {
		path, err := m.fetchAndExtractFunc(ctx, imageURL, outputDir)
		return path, nil, err
	}
	return filepath.Join(outputDir, "bootstrap"), nil, nil
}

func (m *mockNydusClient) UseHTTPFor(imageURL string) bool {
	return m.useHTTPForFunc != nil && m.useHTTPForFunc(imageURL)
}

func createTestOSSAuthsFile(t *testing.T, dir string) string {
	t.Helper()
	authsPath := filepath.Join(dir, "oss_auths.json")
	auths := OSSAuthsConfig{
		"oss-cn-hangzhou.aliyuncs.com/test-bucket": {
			AccessKeyID:     "test-access-key",
			AccessKeySecret: "test-secret-key",
		},
		"oss-cn-beijing.aliyuncs.com/another-bucket": {
			AccessKeyID:     "beijing-key",
			AccessKeySecret: "beijing-secret",
		},
	}
	data, err := json.Marshal(auths)
	if err != nil {
		t.Fatalf("Failed to marshal oss auths: %v", err)
	}
	if err := os.WriteFile(authsPath, data, 0644); err != nil {
		t.Fatalf("Failed to write oss auths file: %v", err)
	}
	return authsPath
}

func createTestRegistryAuthsFile(t *testing.T, dir string) string {
	t.Helper()
	authsPath := filepath.Join(dir, "registry_auths.json")
	auths := registryauth.Config{
		"docker.io": {
			Auth: "base64-dockerhub-auth",
		},
		"reg.docker.alibaba-inc.com/namespace/repo": {
			Auth: "base64-specific-repo-auth",
		},
		"reg.docker.alibaba-inc.com": {
			Auth: "base64-host-auth",
		},
	}
	data, err := json.Marshal(auths)
	if err != nil {
		t.Fatalf("Failed to marshal registry auths: %v", err)
	}
	if err := os.WriteFile(authsPath, data, 0644); err != nil {
		t.Fatalf("Failed to write registry auths file: %v", err)
	}
	return authsPath
}

func createTestDockerFormatRegistryAuthsFile(t *testing.T, dir string) string {
	t.Helper()
	authsPath := filepath.Join(dir, "registry_auths_docker.json")
	auths := map[string]interface{}{
		"auths": map[string]map[string]string{
			"docker.io": {
				"auth": "base64-dockerhub-auth",
			},
			"reg.docker.alibaba-inc.com/namespace/repo": {
				"auth": "base64-specific-repo-auth",
			},
			"reg.docker.alibaba-inc.com": {
				"auth": "base64-host-auth",
			},
		},
		"credsStore": "mock",
	}
	data, err := json.Marshal(auths)
	if err != nil {
		t.Fatalf("Failed to marshal docker-format registry auths: %v", err)
	}
	if err := os.WriteFile(authsPath, data, 0644); err != nil {
		t.Fatalf("Failed to write docker-format registry auths file: %v", err)
	}
	return authsPath
}

func createTestConfigFile(t *testing.T, dir string, filename string, cfg BackendConfig) string {
	t.Helper()
	cfgPath := filepath.Join(dir, filename)
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}
	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		t.Fatalf("Failed to write config file: %v", err)
	}
	return cfgPath
}
