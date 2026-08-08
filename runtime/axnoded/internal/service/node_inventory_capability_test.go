package service

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

func TestRuntimeStorageCapabilitiesPublishCgroupMemoryReadiness(t *testing.T) {
	cfg := config.Config{RootDir: t.TempDir()}
	if containsCapability(runtimeStorageCapabilities(cfg, false), "cgroup:memory-limit-ready") {
		t.Fatal("unverified cgroup memory readiness was published")
	}
	if !containsCapability(runtimeStorageCapabilities(cfg, true), "cgroup:memory-limit-ready") {
		t.Fatal("verified cgroup memory readiness was not published")
	}
}

func containsCapability(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
