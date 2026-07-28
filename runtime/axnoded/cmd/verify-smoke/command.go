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
	address        string
	rootfsSrc      string
	rootfs         string
	s3Endpoint     string
	s3Bucket       string
	s3Object       string
	s3AccessKeyID  string
	s3AccessSecret string
	imageURL       string
	runtimeName    string
	runtimeID      string
	stdoutPath     string
	stderrPath     string
	command        string
	argvJSON       string
	commandShell   string
	expectStdout   string
	expectStderr   string
	expectedExit   int
}

func parseFlags() verifySmokeConfig {
	cfg := verifySmokeConfig{}
	flag.StringVar(&cfg.address, "address", config.DefaultSocketAddress, "axnoded unix socket path")
	flag.StringVar(&cfg.rootfsSrc, "rootfs-src", "local", "rootfs source: local, s3, or image")
	flag.StringVar(&cfg.rootfs, "rootfs", "/opt/sample-rootfs", "LOCAL rootfs path")
	flag.StringVar(&cfg.s3Endpoint, "s3-endpoint", "", "S3/OSS endpoint for rootfs-src=s3")
	flag.StringVar(&cfg.s3Bucket, "s3-bucket", "", "S3/OSS bucket for rootfs-src=s3")
	flag.StringVar(&cfg.s3Object, "s3-object", "", "S3/OSS object for rootfs-src=s3")
	flag.StringVar(&cfg.s3AccessKeyID, "s3-access-key-id", "", "S3/OSS access key id for rootfs-src=s3")
	flag.StringVar(&cfg.s3AccessSecret, "s3-access-key-secret", "", "S3/OSS access key secret for rootfs-src=s3")
	flag.StringVar(&cfg.imageURL, "image-url", "", "OCI/Nydus image URL for rootfs-src=image")
	flag.StringVar(&cfg.runtimeName, "runtime", config.RuntimeNameRunsc, "sandbox runtime name under test")
	flag.StringVar(&cfg.runtimeID, "runtime-id", "verify-runtime", "function runtime id")
	flag.StringVar(&cfg.stdoutPath, "stdout", "/tmp/axnoded-verify.stdout", "container stdout path")
	flag.StringVar(&cfg.stderrPath, "stderr", "/tmp/axnoded-verify.stderr", "container stderr path")
	flag.StringVar(&cfg.command, "command", "", "shell snippet executed as /bin/sh -c ...")
	flag.StringVar(&cfg.argvJSON, "argv-json", "", "full JSON array argv, overrides -command and -command-shell when set")
	flag.StringVar(&cfg.commandShell, "command-shell", "echo generic-axnoded-ok; echo generic-axnoded-err 1>&2; sleep 1", "legacy alias for -command")
	flag.StringVar(&cfg.expectStdout, "expect-stdout", "generic-axnoded-ok", "stdout substring expected after the container exits")
	flag.StringVar(&cfg.expectStderr, "expect-stderr", "generic-axnoded-err", "stderr substring expected after the container exits")
	flag.IntVar(&cfg.expectedExit, "expected-exit", 0, "expected container exit code")
	flag.Parse()
	return cfg
}

func buildExecutionConfig(cfg verifySmokeConfig) (*privatenodev1.ResolvedExecutionConfig, error) {
	commandToRun, err := resolveCommand(cfg.argvJSON, cfg.command, cfg.commandShell)
	if err != nil {
		return nil, err
	}
	rootfsSpec, err := buildRootfsSpec(cfg.rootfsSrc, cfg.rootfs, cfg.imageURL, cfg.s3Endpoint, cfg.s3Bucket, cfg.s3Object, cfg.s3AccessKeyID, cfg.s3AccessSecret)
	if err != nil {
		return nil, err
	}
	spec := &privatenodev1.ResolvedExecutionConfig{
		RuntimeClass: cfg.runtimeName,
		Argv:         commandToRun,
		Cwd:          "/",
		StdoutPath:   cfg.stdoutPath,
		StderrPath:   cfg.stderrPath,
	}
	rootfsSpec.Apply(spec)
	return spec, nil
}

func resolveCommand(argvJSON, shellSnippet, legacyShellSnippet string) ([]string, error) {
	return verifyutil.ResolveArgv(argvJSON, shellSnippet, legacyShellSnippet)
}

func buildRootfsSpec(src, localPath, imageURL, endpoint, bucket, object, accessKeyID, accessKeySecret string) (*verifyutil.RootfsSpec, error) {
	return verifyutil.BuildRootfsSpec(src, localPath, imageURL, endpoint, bucket, object, accessKeyID, accessKeySecret)
}
