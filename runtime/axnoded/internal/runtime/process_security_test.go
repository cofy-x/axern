package runtime

import (
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"os"
	"path/filepath"
	"testing"
)

func TestLegacyProcessProfileRetainsCleanupProjectionButCannotActivate(t *testing.T) {
	handler, manifest, executor := taskEnforcementFixture(t)
	legacy := proto.Clone(manifest).(*apipb.AllocationEnforcementManifest)
	legacy.ProcessSecurityProfile = ""
	writeTaskManifestFixture(t, legacy)
	path := filepath.Join(legacy.BundlePath, "config.json")
	weak := []byte(`{"process":{"noNewPrivileges":false,"capabilities":{}}}`)
	require.NoError(t, os.WriteFile(path, weak, 0600))
	raw, err := os.ReadFile(filepath.Join(legacy.BundlePath, runtimeEnforcementManifestFile))
	require.NoError(t, err)
	require.ErrorContains(t, handler.VerifyAllocationTaskLimits(t.Context(), "task-allocation", legacy), "predates the managed-process security policy")
	require.Error(t, handler.ResumeContainer(t.Context(), contract.HandlerOptions{ContainerID: "task-allocation", EnforcementManifest: legacy}))
	require.Empty(t, executor.Args(), "rejected legacy authority must never issue a runtime command")
	projection, err := handler.AllocationEnforcementManifest(t.Context(), "task-allocation")
	require.NoError(t, err)
	require.True(t, proto.Equal(legacy, projection), "the original cleanup authority remains accessible")
	require.NoError(t, contract.ValidateEnforcementManifest(projection, legacy.BundlePath))
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, weak, after, "activation never rewrites a live spec")
	after, err = os.ReadFile(filepath.Join(legacy.BundlePath, runtimeEnforcementManifestFile))
	require.NoError(t, err)
	require.Equal(t, raw, after, "ownership/cleanup debt is retained")
}

func TestCurrentProcessProfileCannotAdoptAWeakPersistedSpec(t *testing.T) {
	for _, weak := range []string{
		`{"process":{"noNewPrivileges":false,"capabilities":{}}}`,
		`{"process":{"noNewPrivileges":true,"user":{"uid":1000,"gid":0},"capabilities":{"bounding":["CAP_CHOWN"]}}}`,
		`{"process":{"noNewPrivileges":true,"user":{"uid":1000,"gid":0,"additionalGids":[0]},"capabilities":{}}}`,
	} {
		handler, manifest, executor := taskEnforcementFixture(t)
		path := filepath.Join(manifest.BundlePath, "config.json")
		require.NoError(t, os.WriteFile(path, []byte(weak), 0600))
		require.Error(t, handler.VerifyAllocationTaskLimits(t.Context(), "task-allocation", manifest))
		require.Empty(t, executor.Args())
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, weak, string(after))
	}
}
