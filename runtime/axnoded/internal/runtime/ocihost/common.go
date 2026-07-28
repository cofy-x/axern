package ocihost

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/execflow"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/internal/ocicli"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
)

const runtimeExitStateStoreDirName = "runtime-exit-states"

type Common struct {
	binary              string
	runtimeRunnerBinary string
	containerRoot       string
	runtimeRoot         string
	exitStateRoot       string
	executor            Executor
	ociLoader           runtimeoci.Loader
}

type Config struct {
	Root                string
	RuntimeName         string
	RuntimeBinary       string
	RuntimeRunnerBinary string
	Loader              runtimeoci.Loader
}

func New(cfg Config) (*Common, error) {
	runtimeRoot, containerRoot, err := ocicli.EnsureRuntimeDirs(cfg.Root, cfg.RuntimeName)
	if err != nil {
		return nil, err
	}
	if err := os.RemoveAll(filepath.Join(runtimeRoot, runtimeExitStateStoreDirName)); err != nil {
		return nil, err
	}
	return &Common{
		binary:              cfg.RuntimeBinary,
		runtimeRunnerBinary: cfg.RuntimeRunnerBinary,
		containerRoot:       containerRoot,
		runtimeRoot:         runtimeRoot,
		exitStateRoot:       filepath.Join(cfg.Root, runtimeExitStateStoreDirName, cfg.RuntimeName),
		executor:            &SystemExecutor{},
		ociLoader:           cfg.Loader,
	}, nil
}

func (c *Common) SetExecutor(executor Executor) {
	if executor == nil {
		c.executor = &SystemExecutor{}
		return
	}
	c.executor = executor
}

func (c *Common) SetRuntimeRunnerBinary(path string) {
	c.runtimeRunnerBinary = path
}

func (c *Common) Binary() string {
	return c.binary
}

func (c *Common) RuntimeRunnerBinary() string {
	return c.runtimeRunnerBinary
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
	return ocicli.RunWithIO(ctx, c.binary, c.runtimeRoot, stdoutPath, stderrPath, args...)
}

func (c *Common) NewCommandContext(ctx context.Context, args ...string) *exec.Cmd {
	return ocicli.NewCommandContext(ctx, c.binary, c.runtimeRoot, args...)
}

func (c *Common) NewExecProcessState(containerID string, requestCommand []string, envs map[string]string, cwd string, user string, terminal bool) (*execflow.ProcessState, error) {
	return execflow.NewExecProcessState(c.ContainerSpec, c.ContainerPath, containerID, requestCommand, envs, cwd, user, terminal)
}
