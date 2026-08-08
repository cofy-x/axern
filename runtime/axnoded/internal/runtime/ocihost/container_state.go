package ocihost

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/sirupsen/logrus"
)

func (c *Common) ContainerPath(containerID string) string {
	return filepath.Join(c.containerRoot, containerID)
}

func (c *Common) EnsureContainerPath(containerID string) error {
	return os.MkdirAll(c.ContainerPath(containerID), 0755)
}

func (c *Common) RuntimeExitStatePath(containerID string) string {
	return filepath.Join(c.exitStateRoot, containerID+".json")
}

func (c *Common) RemoveExitState(containerID string) error {
	err := os.Remove(c.RuntimeExitStatePath(containerID))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (c *Common) RuntimePIDFilePath(containerID string) string {
	return filepath.Join(c.ContainerPath(containerID), "runtime.pid")
}

func (c *Common) Version(ctx context.Context) (string, error) {
	output, err := c.Run(ctx, "--version")
	if err != nil {
		return "", err
	}
	return strings.Trim(strings.ReplaceAll(string(output), "\n", " - "), " - "), nil
}

func (c *Common) ListContainers(ctx context.Context) ([]*contract.UnionContainerState, error) {
	output, err := c.Run(ctx, "list", "--format", "json")
	if err != nil {
		return []*contract.UnionContainerState{}, err
	}
	return parseContainerListOutput(output)
}

func parseContainerListOutput(output []byte) ([]*contract.UnionContainerState, error) {
	containers := make([]*contract.UnionContainerState, 0)
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return containers, nil
	}
	if trimmed == "null" {
		return containers, nil
	}
	start := strings.IndexByte(trimmed, '[')
	end := strings.LastIndexByte(trimmed, ']')
	if start < 0 || end < start {
		return containers, fmt.Errorf("decode OCI container list: missing JSON array in %q", trimmed)
	}
	if err := json.Unmarshal([]byte(trimmed[start:end+1]), &containers); err != nil {
		return containers, fmt.Errorf("decode OCI container list: %w", err)
	}
	return containers, nil
}

func (c *Common) ContainerSpec(containerID string) (*spec.Spec, error) {
	specPath := filepath.Join(c.containerRoot, containerID, config.ContainerSpecFile)
	data, err := os.ReadFile(specPath)
	if err != nil {
		return nil, err
	}
	var out spec.Spec
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Common) CleanupOnFailure(ctx context.Context, traceID, containerID, msg string) error {
	logrus.WithField("trace_id", traceID).Warn(msg)
	_, err := c.Run(ctx, "delete", "--force", containerID)
	if err != nil && strings.Contains(err.Error(), "file does not exist") {
		return nil
	}
	return err
}

func ParseWaitExitCode(output []byte) (int, error) {
	return ocicli.ParseWaitExitCode(output)
}
