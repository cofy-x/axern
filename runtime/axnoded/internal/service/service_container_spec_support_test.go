package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	specs "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/assert"
)

func writeContainerSpecFile(t *testing.T, rootDir, containerID string, annotations map[string]string) {
	t.Helper()
	containerDir := filepath.Join(rootDir, "containers", containerID)
	assert.NoError(t, os.MkdirAll(containerDir, 0755))

	specBytes, err := json.Marshal(&specs.Spec{
		Version:     "1.0.0",
		Annotations: annotations,
		Linux:       &specs.Linux{CgroupsPath: ""},
		Process:     &specs.Process{Cwd: "/tmp"},
	})
	assert.NoError(t, err)
	assert.NoError(t, os.WriteFile(filepath.Join(containerDir, config.ContainerSpecFile), specBytes, 0644))
}
