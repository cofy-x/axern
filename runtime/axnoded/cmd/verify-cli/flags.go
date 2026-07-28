package main

import (
	"flag"
	"fmt"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/google/uuid"
)

type verifyCLIConfig struct {
	address           string
	runtime           string
	runtimeID         string
	rootfsSrc         string
	rootfsPath        string
	imageURL          string
	s3Endpoint        string
	s3Bucket          string
	s3Object          string
	s3AccessKeyID     string
	s3AccessKeySecret string
	stdoutPath        string
	stderrPath        string
	shellCommand      string
	requestCPUMilli   float64
	requestMemoryMiB  float64
	limitCPUMilli     float64
	limitMemoryMiB    float64
	createTimeout     time.Duration
	allocationAttempt int64
	userEnvFlags      verifyutil.StringSliceFlag
	mountFlags        verifyutil.StringSliceFlag
}

func parseFlags() verifyCLIConfig {
	cfg := verifyCLIConfig{}
	flag.StringVar(&cfg.address, "address", config.DefaultSocketAddress, "axnoded unix socket path")
	flag.StringVar(&cfg.runtime, "runtime", config.RuntimeNameRunsc, "sandbox runtime name under test")
	flag.StringVar(&cfg.runtimeID, "runtime-id", "", "function runtime id")
	flag.StringVar(&cfg.rootfsSrc, "rootfs-src", "local", "rootfs source: local, image, or s3")
	flag.StringVar(&cfg.rootfsPath, "rootfs", "/opt/sample-rootfs", "LOCAL rootfs path")
	flag.StringVar(&cfg.imageURL, "image-url", "", "OCI/Nydus image URL for rootfs-src=image")
	flag.StringVar(&cfg.s3Endpoint, "s3-endpoint", "", "S3/OSS endpoint for rootfs-src=s3")
	flag.StringVar(&cfg.s3Bucket, "s3-bucket", "", "S3/OSS bucket for rootfs-src=s3")
	flag.StringVar(&cfg.s3Object, "s3-object", "", "S3/OSS object for rootfs-src=s3")
	flag.StringVar(&cfg.s3AccessKeyID, "s3-access-key-id", "", "S3/OSS access key id for rootfs-src=s3")
	flag.StringVar(&cfg.s3AccessKeySecret, "s3-access-key-secret", "", "S3/OSS access key secret for rootfs-src=s3")
	flag.StringVar(&cfg.stdoutPath, "stdout", "/tmp/axnoded-cli.stdout", "container stdout path")
	flag.StringVar(&cfg.stderrPath, "stderr", "/tmp/axnoded-cli.stderr", "container stderr path")
	flag.StringVar(&cfg.shellCommand, "shell-command", "sleep 300", "shell command to run inside the container")
	flag.Float64Var(&cfg.requestCPUMilli, "request-cpu-milli", 0, "requested CPU in milli-CPU units for StartRequest resources")
	flag.Float64Var(&cfg.requestMemoryMiB, "request-memory-mib", 0, "requested memory in MiB for StartRequest resources")
	flag.Float64Var(&cfg.limitCPUMilli, "limit-cpu-milli", 0, "CPU limit in milli-CPU units for StartRequest resources")
	flag.Float64Var(&cfg.limitMemoryMiB, "limit-memory-mib", 0, "memory limit in MiB for StartRequest resources")
	flag.DurationVar(&cfg.createTimeout, "create-timeout", defaultCreateSandboxTimeout, "timeout for CreateAllocation")
	flag.Int64Var(&cfg.allocationAttempt, "allocation-attempt", 1, "allocation attempt; use 0 only for static execution-envelope verification")
	flag.Var(&cfg.userEnvFlags, "user-env", "dynamic user env in KEY=VALUE form (repeatable)")
	flag.Var(&cfg.mountFlags, "mount", "dynamic bind mount in SOURCE:TARGET[:options] form (repeatable)")
	flag.Parse()

	if cfg.runtimeID == "" {
		cfg.runtimeID = fmt.Sprintf("verify-cli-%s", uuid.NewString())
	}
	return cfg
}
