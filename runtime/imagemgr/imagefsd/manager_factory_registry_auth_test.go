package imagefsd

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestManager_CreateDaemon_Nydus_DockerAuthsFormat(t *testing.T) {
	tmpDir := t.TempDir()

	ossConfig := BackendConfig{
		BackendType: "oss",
		Oss:         &OssConfig{},
	}
	ossCfgPath := createTestConfigFile(t, tmpDir, "oss_config.json", ossConfig)

	nydusConfig := BackendConfig{
		BackendType: "registry",
		Registry: &RegistryConfig{
			Host:   "docker.io",
			Scheme: "https",
			Auth:   "",
		},
	}
	nydusCfgPath := createTestConfigFile(t, tmpDir, "nydus_config.json", nydusConfig)

	ossAuthsPath := createTestOSSAuthsFile(t, tmpDir)
	registryAuthsPath := createTestDockerFormatRegistryAuthsFile(t, tmpDir)

	mgr, err := NewManager(&ManagerConfig{
		NodeID:            "node-test",
		Root:              tmpDir,
		OSSCfgPath:        ossCfgPath,
		NydusCfgPath:      nydusCfgPath,
		BinPath:           "/usr/local/bin/imagefsd",
		OSSAuthsPath:      ossAuthsPath,
		RegistryAuthsPath: registryAuthsPath,
	})
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}

	opts := &DaemonCreateOpt{
		ID:         "nydus-daemon-docker-auths",
		Name:       "nydus-image",
		SourceType: "nydus",
		ImageURL:   "reg.docker.alibaba-inc.com/namespace/repo:latest",
	}
	if err := mgr.CreateDaemon(opts); err != nil {
		t.Fatalf("CreateDaemon() failed: %v", err)
	}

	d := mgr.GetDaemon(opts.ID)
	if d == nil {
		t.Fatal("Created daemon not found")
	}
	if d.config.Registry.Auth != "base64-specific-repo-auth" {
		t.Errorf("Registry.Auth = %s, want base64-specific-repo-auth", d.config.Registry.Auth)
	}
}

func TestManager_CreateDaemon_Nydus_RequestAuthOverridesNodeAuth(t *testing.T) {
	tmpDir := t.TempDir()
	ossCfgPath := createTestConfigFile(t, tmpDir, "oss_config.json", BackendConfig{
		BackendType: "oss",
		Oss:         &OssConfig{},
	})
	nydusCfgPath := createTestConfigFile(t, tmpDir, "nydus_config.json", BackendConfig{
		BackendType: "registry",
		Registry:    &RegistryConfig{Scheme: "https"},
	})

	mgr, err := NewManager(&ManagerConfig{
		NodeID:            "node-test",
		Root:              tmpDir,
		OSSCfgPath:        ossCfgPath,
		NydusCfgPath:      nydusCfgPath,
		BinPath:           "/usr/local/bin/imagefsd",
		OSSAuthsPath:      createTestOSSAuthsFile(t, tmpDir),
		RegistryAuthsPath: createTestDockerFormatRegistryAuthsFile(t, tmpDir),
	})
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}

	const dockerConfig = `{"auths":{"registry.example":{"auth":"request-auth"}}}`
	opts := &DaemonCreateOpt{
		ID:               "private-nydus",
		Name:             "nydus-image",
		SourceType:       SourceTypeNydus,
		ImageURL:         "registry.example/ns/repo:latest",
		RegistryAuth:     "request-auth",
		DockerConfigJSON: dockerConfig,
	}
	if err := mgr.CreateDaemon(opts); err != nil {
		t.Fatalf("CreateDaemon() error = %v", err)
	}
	d := mgr.GetDaemon(opts.ID)
	if got := d.config.Registry.Auth; got != "request-auth" {
		t.Fatalf("Registry.Auth = %q, want request-auth", got)
	}
	if got := d.dockerConfigJSON; got != dockerConfig {
		t.Fatal("request-scoped Docker config was not retained for bootstrap initialization")
	}
	raw, err := json.Marshal(d.meta)
	if err != nil {
		t.Fatalf("marshal daemon metadata: %v", err)
	}
	if strings.Contains(string(raw), "request-auth") {
		t.Fatal("request-scoped Docker config leaked into daemon metadata")
	}
}
