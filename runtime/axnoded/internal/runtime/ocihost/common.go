package ocihost

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/execflow"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

const runtimeExitStateStoreDirName = "runtime-exit-states"
const runtimeName = "runsc"

type Common struct {
	binary        string
	containerRoot string
	runtimeRoot   string
	exitStateRoot string
	executor      Executor
	ociLoader     runtimeoci.Loader
}

type Config struct {
	Root          string
	RuntimeBinary string
	Loader        runtimeoci.Loader
}

func New(cfg Config) (*Common, error) {
	runtimeRoot, containerRoot, err := ocicli.EnsureRuntimeDirs(cfg.Root, runtimeName)
	if err != nil {
		return nil, err
	}
	exitStateRoot := filepath.Join(cfg.Root, runtimeExitStateStoreDirName, runtimeName)
	if err := os.MkdirAll(exitStateRoot, 0755); err != nil {
		return nil, err
	}
	return &Common{
		binary:        cfg.RuntimeBinary,
		containerRoot: containerRoot,
		runtimeRoot:   runtimeRoot,
		exitStateRoot: exitStateRoot,
		executor:      &SystemExecutor{},
		ociLoader:     cfg.Loader,
	}, nil
}

func (c *Common) SetExecutor(executor Executor) {
	if executor == nil {
		c.executor = &SystemExecutor{}
		return
	}
	c.executor = executor
}

func (c *Common) Binary() string {
	return c.binary
}

func (c *Common) ContainerRoot() string {
	return c.containerRoot
}

func (c *Common) Loader() runtimeoci.Loader {
	return c.ociLoader
}

func (c *Common) CommandArgs(args ...string) []string {
	return ocicli.CommandArgs(c.runtimeRoot, args...)
}

func (c *Common) Run(ctx context.Context, args ...string) ([]byte, error) {
	return c.executor.Execute(ctx, c.binary, c.CommandArgs(args...)...)
}

func (c *Common) RunWithIO(ctx context.Context, stdoutPath, stderrPath string, args ...string) error {
	executor, ok := c.executor.(IOExecutor)
	if !ok {
		return fmt.Errorf("runtime executor does not support inherited stdio")
	}
	return executor.ExecuteWithIO(ctx, c.binary, c.runtimeRoot, stdoutPath, stderrPath, args...)
}

func (c *Common) NewCommandContext(ctx context.Context, args ...string) *exec.Cmd {
	return ocicli.NewCommandContext(ctx, c.binary, c.runtimeRoot, args...)
}

func (c *Common) NewExecProcessState(containerID string, requestCommand []string, envs map[string]string, cwd string, user string, terminal bool) (*execflow.ProcessState, error) {
	return execflow.NewExecProcessState(c.ContainerSpec, c.ContainerPath, containerID, requestCommand, envs, cwd, user, terminal)
}
