package main

import (
	"flag"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
)

const (
	registerTimeout = 5 * time.Minute
	startTimeout    = 5 * time.Minute
	listTimeout     = 30 * time.Second
	waitTimeout     = 5 * time.Minute
	deleteTimeout   = 30 * time.Second
)

type verifySmokeConfig struct {
	address      string
	rootfsSrc    string
	rootfs       string
	imageURL     string
	runtimeName  string
	runtimeID    string
	stdoutPath   string
	stderrPath   string
	command      string
	argvJSON     string
	expectStdout string
	expectStderr string
	expectedExit int
}

func parseFlags() verifySmokeConfig {
	cfg := verifySmokeConfig{}
	flag.StringVar(&cfg.address, "address", config.DefaultSocketAddress, "axnoded unix socket path")
	flag.StringVar(&cfg.rootfsSrc, "rootfs-src", "local", "rootfs source: local or image")
	flag.StringVar(&cfg.rootfs, "rootfs", "/opt/sample-rootfs", "LOCAL rootfs path")
	flag.StringVar(&cfg.imageURL, "image-url", "", "OCI/Nydus image URL for rootfs-src=image")
	flag.StringVar(&cfg.runtimeName, "runtime", config.RuntimeNameRunsc, "sandbox runtime name under test")
	flag.StringVar(&cfg.runtimeID, "runtime-id", "verify-runtime", "runtime id")
	flag.StringVar(&cfg.stdoutPath, "stdout", "/tmp/axnoded-verify.stdout", "container stdout path")
	flag.StringVar(&cfg.stderrPath, "stderr", "/tmp/axnoded-verify.stderr", "container stderr path")
	flag.StringVar(&cfg.command, "command", "echo generic-axnoded-ok; echo generic-axnoded-err 1>&2; sleep 1", "shell snippet executed as /bin/sh -c ...")
	flag.StringVar(&cfg.argvJSON, "argv-json", "", "full JSON array argv, overrides -command when set")
	flag.StringVar(&cfg.expectStdout, "expect-stdout", "generic-axnoded-ok", "stdout substring expected after the container exits")
	flag.StringVar(&cfg.expectStderr, "expect-stderr", "generic-axnoded-err", "stderr substring expected after the container exits")
	flag.IntVar(&cfg.expectedExit, "expected-exit", 0, "expected container exit code")
	flag.Parse()
	return cfg
}

func buildExecutionConfig(cfg verifySmokeConfig) (*privatenodev1.ResolvedExecutionConfig, error) {
	commandToRun, err := resolveCommand(cfg.argvJSON, cfg.command)
	if err != nil {
		return nil, err
	}
	rootfsSpec, err := buildRootfsSpec(cfg.rootfsSrc, cfg.rootfs, cfg.imageURL)
	if err != nil {
		return nil, err
	}
	spec := &privatenodev1.ResolvedExecutionConfig{
		Argv:       commandToRun,
		Cwd:        "/",
		StdoutPath: cfg.stdoutPath,
		StderrPath: cfg.stderrPath,
	}
	rootfsSpec.Apply(spec)
	return spec, nil
}

func resolveCommand(argvJSON, shellSnippet string) ([]string, error) {
	return verifyutil.ResolveArgv(argvJSON, shellSnippet, "")
}

func buildRootfsSpec(src, localPath, imageURL string) (*verifyutil.RootfsSpec, error) {
	return verifyutil.BuildRootfsSpec(src, localPath, imageURL)
}
