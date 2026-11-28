package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/internal/proto/nativebackup"
	"github.com/stretchr/testify/require"
)

func TestNativeReplacementRotatesOwnershipAndRetainsWriterGeneration(t *testing.T) {
	manager, _, spec := newFixture(t)
	observed, err := manager.Provision(spec)
	require.NoError(t, err)
	binding := Binding{Spec: spec, Identity: observed.Identity, Generation: 1}
	directory, err := manager.Attach(binding, "allocation-one")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(directory, "retained"), []byte("data"), 0600))
	plan := nativebackup.Plan{Format: nativebackup.Format, OldNode: spec.NodeID, NewNode: "node-replacement",
		Workspaces: []nativebackup.Workspace{{ID: spec.ID, Namespace: spec.Namespace, RootIdentity: spec.RootIdentity,
			OldIdentity: observed.Identity, Bytes: spec.Capacity, Inodes: spec.Inodes, Generation: 1}}}
	_, err = manager.RemapRetained(plan, false)
	require.ErrorContains(t, err, "drained")
	receipt, err := manager.Release(binding, "allocation-one")
	require.NoError(t, err)
	require.NoError(t, manager.Acknowledge(receipt))

	mapped, err := manager.RemapRetained(plan, false)
	require.NoError(t, err)
	require.NotEqual(t, observed.Identity, mapped.Workspaces[0].NewIdentity)
	bad := mapped
	bad.Workspaces = []nativebackup.Workspace{mapped.Workspaces[0]}
	bad.Workspaces[0].Generation = 0
	_, err = manager.RemapRetained(bad, true)
	require.ErrorContains(t, err, "drained")
	_, err = manager.Inspect(spec, observed.Identity)
	require.NoError(t, err)

	for i := 0; i < 2; i++ {
		_, err = manager.RemapRetained(mapped, true)
		require.NoError(t, err)
	}
	_, err = manager.Attach(Binding{Spec: spec, Identity: observed.Identity, Generation: 2}, "stale-node")
	require.Error(t, err)

	replacement := New(manager.options)
	replacement.os = manager.os
	replacement.options.NodeID = plan.NewNode
	spec.NodeID = plan.NewNode
	binding = Binding{Spec: spec, Identity: mapped.Workspaces[0].NewIdentity, Generation: 1}
	_, err = replacement.Attach(binding, "replacement-allocation")
	require.ErrorContains(t, err, "retired")
	binding.Generation = 2
	root, err := replacement.Attach(binding, "replacement-allocation")
	require.NoError(t, err)
	retained, err := os.ReadFile(filepath.Join(root, "retained"))
	require.NoError(t, err)
	require.Equal(t, []byte("data"), retained)
}
