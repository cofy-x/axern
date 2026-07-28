package main

import (
	"github.com/cofy-x/axern/runtime/axnoded/cmd/internal/verifyutil"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
)

func buildStartResources(cfg verifyCLIConfig) *commonv1.ResourceSpec {
	return verifyutil.BuildSandboxResources(cfg.requestCPUMilli, cfg.requestMemoryMiB, cfg.limitCPUMilli, cfg.limitMemoryMiB)
}

func buildDynamicOptions(cfg verifyCLIConfig) (map[string]string, []*privatenodev1.SandboxMount, error) {
	userEnvs, err := verifyutil.ParseUserEnvs(cfg.userEnvFlags)
	if err != nil {
		return nil, nil, err
	}
	mounts, err := verifyutil.ParseMounts(cfg.mountFlags)
	if err != nil {
		return nil, nil, err
	}
	return userEnvs, mounts, nil
}
