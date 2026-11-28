package pgrun

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	pgadmin "github.com/cofy-x/axern/control/controld/internal/postgres/admin"
	"github.com/cofy-x/axern/internal/proto/nativebackup"
	workspacev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/workspace/v1"
	"github.com/stretchr/testify/require"
)

func TestNativeRestoreMovesWorkspaceAndRevokesOldAuthorityAtomically(t *testing.T) {
	f := newWorkspaceFixture(t)
	store := pgadmin.NewStore(f.db)
	oldNode, newNode := "workspace-node", "replacement-node"
	rootID, oldIdentity, newIdentity := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	token := strings.Repeat("d", 64)
	_, err := f.db.Pool().Exec(f.ctx, `UPDATE workspaces SET storage_root_identity=$2,storage_identity=$3,generation=3 WHERE workspace_id=$1`,
		f.workspace.GetID(), rootID, oldIdentity)
	require.NoError(t, err)
	_, err = f.db.Pool().Exec(f.ctx, `INSERT INTO nodes(node_id,node_target,enrollment_token_hash,admitted_at,lifecycle_status)
		VALUES('other-node','',repeat('0',64),$1,'active')`, f.now)
	require.NoError(t, err)
	_, err = store.PlanNativeRestore(f.ctx, oldNode, newNode)
	require.ErrorContains(t, err, "fully drained")
	_, err = f.db.Pool().Exec(f.ctx, `DELETE FROM nodes WHERE node_id='other-node'`)
	require.NoError(t, err)

	plan, err := store.PlanNativeRestore(f.ctx, oldNode, newNode)
	require.NoError(t, err)
	require.Len(t, plan.Workspaces, 1)
	require.Equal(t, int64(3), plan.Workspaces[0].Generation)
	plan.Workspaces[0].NewIdentity = newIdentity
	wrong := plan
	wrong.Workspaces = []nativebackup.Workspace{plan.Workspaces[0]}
	wrong.Workspaces[0].Bytes += 512
	require.ErrorContains(t, store.CommitNativeRestore(f.ctx, wrong, token, f.now), "inventory changed")
	empty := plan
	empty.Workspaces = []nativebackup.Workspace{}
	require.ErrorContains(t, store.CommitNativeRestore(f.ctx, empty, token, f.now), "inventory changed")
	current, err := f.workspaces.GetWorkspace(f.ctx, f.workspace.GetID())
	require.NoError(t, err)
	require.Equal(t, oldNode, current.GetNodeID())

	for i := 0; i < 2; i++ {
		require.NoError(t, store.CommitNativeRestore(f.ctx, plan, token, f.now))
	}
	require.Error(t, store.CommitNativeRestore(f.ctx, plan, strings.Repeat("e", 64), f.now))
	current, err = f.workspaces.GetWorkspace(f.ctx, f.workspace.GetID())
	require.NoError(t, err)
	require.Equal(t, newNode, current.GetNodeID())
	require.Equal(t, int64(3), current.GetGeneration())
	var lifecycle, identity string
	require.NoError(t, f.db.Pool().QueryRow(f.ctx, `SELECT lifecycle_status FROM nodes WHERE node_id=$1`, oldNode).Scan(&lifecycle))
	require.Equal(t, "revoked", lifecycle)
	require.NoError(t, f.db.Pool().QueryRow(f.ctx, `SELECT storage_identity FROM workspaces WHERE workspace_id=$1`, current.GetID()).Scan(&identity))
	require.Equal(t, newIdentity, identity)
}

func TestNativeRestoreCommitsAfterLastWorkspaceIsDeleted(t *testing.T) {
	f := newWorkspaceFixture(t)
	store := pgadmin.NewStore(f.db)
	oldNode, newNode := "workspace-node", "replacement-node"
	token := strings.Repeat("d", 64)
	_, err := f.db.Pool().Exec(f.ctx, `UPDATE workspaces SET storage_root_identity=$2,storage_identity=$3 WHERE workspace_id=$1`,
		f.workspace.GetID(), strings.Repeat("a", 64), strings.Repeat("b", 64))
	require.NoError(t, err)
	stale, err := store.PlanNativeRestore(f.ctx, oldNode, newNode)
	require.NoError(t, err)
	require.Len(t, stale.Workspaces, 1)
	stale.Workspaces[0].NewIdentity = strings.Repeat("c", 64)

	_, err = f.workspaces.DeleteWorkspace(f.ctx, f.workspace.GetID(), f.now.Add(time.Second))
	require.NoError(t, err)
	deletedAt := f.now.Add(2 * time.Second)
	item, err := f.workspaces.ClaimWorkspaceReconcile(f.ctx, "restore-fixture-deleter", deletedAt)
	require.NoError(t, err)
	require.NotNil(t, item)
	require.NoError(t, f.workspaces.CompleteWorkspaceReconcile(f.ctx, item, stale.Workspaces[0].OldIdentity, 0, 0, nil, deletedAt))
	require.ErrorContains(t, store.CommitNativeRestore(f.ctx, stale, token, deletedAt), "inventory changed")

	plan, err := store.PlanNativeRestore(f.ctx, oldNode, newNode)
	require.NoError(t, err)
	require.NotNil(t, plan.Workspaces)
	require.Empty(t, plan.Workspaces)
	raw, err := json.Marshal(plan)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"workspaces":[]`)
	mapped, err := nativebackup.Decode(bytes.NewReader(raw), true)
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, store.CommitNativeRestore(f.ctx, mapped, token, deletedAt))
	}
	require.Error(t, store.CommitNativeRestore(f.ctx, mapped, strings.Repeat("e", 64), deletedAt))

	deleted, err := f.workspaces.GetWorkspace(f.ctx, f.workspace.GetID())
	require.NoError(t, err)
	require.Equal(t, workspacev1.WorkspaceStatus_WORKSPACE_STATUS_DELETED, deleted.GetStatus())
	require.Equal(t, oldNode, deleted.GetNodeID())
	var original, replacement string
	require.NoError(t, f.db.Pool().QueryRow(f.ctx, `SELECT lifecycle_status FROM nodes WHERE node_id=$1`, oldNode).Scan(&original))
	require.NoError(t, f.db.Pool().QueryRow(f.ctx, `SELECT lifecycle_status FROM nodes WHERE node_id=$1`, newNode).Scan(&replacement))
	require.Equal(t, "revoked", original)
	require.Equal(t, "active", replacement)
}

func TestNativeUpgradeChecksMigrationHistoryAndOutstandingWriter(t *testing.T) {
	f := newWorkspaceFixture(t)
	store := pgadmin.NewStore(f.db)
	require.NoError(t, f.db.CheckUpgradeMigrations(f.ctx))
	require.NoError(t, store.RequireNativeWorkDrained(f.ctx, "workspace-node"))
	require.ErrorContains(t, store.RequireNativeWorkDrained(f.ctx, "missing-node"), "fully drained")
	_, err := f.store.AdmitRun(f.ctx, f.params, f.now)
	require.NoError(t, err)
	require.ErrorContains(t, store.RequireNativeWorkDrained(f.ctx, "workspace-node"), "fully drained")
	_, err = f.db.Pool().Exec(f.ctx, `INSERT INTO schema_migrations(version,name,checksum) VALUES(999999,'future','unknown')`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = f.db.Pool().Exec(f.ctx, `DELETE FROM schema_migrations WHERE version=999999`) })
	require.ErrorContains(t, f.db.CheckUpgradeMigrations(f.ctx), "supported migration history")
}
