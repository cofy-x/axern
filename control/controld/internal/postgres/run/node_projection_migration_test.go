package pgrun

import (
	"bytes"
	"os"
	"testing"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	"github.com/cofy-x/axern/control/controld/internal/placement"
	pgnodes "github.com/cofy-x/axern/control/controld/internal/postgres/nodes"
	nodev1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/control/node/v1"
	"google.golang.org/protobuf/proto"
)

func TestRefreshNodeProjectionPreservesWorkspaceAndExecutionAuthority(t *testing.T) {
	f := newWorkspaceFixture(t)
	run, err := f.store.AdmitRun(f.ctx, f.params, f.now)
	if err != nil {
		t.Fatal(err)
	}
	const authorityQuery = `SELECT jsonb_build_object(
		'node',(SELECT to_jsonb(n) FROM nodes n WHERE node_id='workspace-node'),
		'workspace',(SELECT to_jsonb(w) FROM workspaces w WHERE workspace_id=$1),
		'run',(SELECT to_jsonb(r) FROM runs r WHERE run_id=$2),
		'allocation',(SELECT to_jsonb(a) FROM allocations a WHERE allocation_id=$3),
		'quotas',(SELECT jsonb_agg(to_jsonb(q)) FROM namespace_resource_quotas q),
		'credential',(SELECT to_jsonb(c) FROM principal_credentials c WHERE credential_id='workspace-credential'))`
	readAuthority := func() []byte {
		t.Helper()
		var data []byte
		if err := f.db.Pool().QueryRow(f.ctx, authorityQuery, f.workspace.GetID(), run.GetID(), run.GetAllocationID()).Scan(&data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	before := readAuthority()
	if _, err := f.db.Pool().Exec(f.ctx, `UPDATE node_summaries SET summary=jsonb_set(summary,'{components,imagefsd}','{"reachable":true}'::jsonb) WHERE node_id='workspace-node'`); err != nil {
		t.Fatal(err)
	}
	migration, err := os.ReadFile("../migrations/000003_refresh_node_projections.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Pool().Exec(f.ctx, string(migration)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, readAuthority()) {
		t.Fatal("refreshing derived observations changed durable authority or retained charge")
	}
	var summaries int
	if err := f.db.Pool().QueryRow(f.ctx, `SELECT count(*) FROM node_summaries`).Scan(&summaries); err != nil || summaries != 0 {
		t.Fatalf("obsolete node summaries=%d error=%v", summaries, err)
	}
	record := *f.params.Candidates[0].Record
	record.Summary = nil
	engine := placement.NewEngine(placement.Config{})
	if got := engine.Evaluate(&record, f.params.Candidates[0].BaseRequest, f.now); got.GetState() != placementkernel.CandidateStateRejected {
		t.Fatal("a node without a fresh OCI snapshot became eligible")
	}
	summary := proto.Clone(f.params.Candidates[0].Summary).(*nodev1.NodeSummary)
	summary.NodeInstanceID, summary.Sequence = "oci-report-generation", 1
	reported, err := pgnodes.NewPGStore(f.db).Report(f.ctx, nodekernel.ReportParams{NodeID: record.NodeID, NodeTarget: record.NodeTarget, Summary: summary, Now: f.now})
	if err != nil {
		t.Fatal(err)
	}
	if got := engine.Evaluate(reported, f.params.Candidates[0].BaseRequest, f.now); got.GetState() != placementkernel.CandidateStateEligible {
		t.Fatalf("fresh OCI snapshot did not restore eligibility: %v", got.GetRejectionReasons())
	}
	w, err := f.workspaces.GetWorkspace(f.ctx, f.workspace.GetID())
	if err != nil || w.GetAttachedAllocationID() != run.GetAllocationID() || w.GetGeneration() != 1 || w.GetCapacityBytes() != f.workspace.GetCapacityBytes() {
		t.Fatalf("projection refresh changed Workspace ownership: workspace=%v error=%v", w, err)
	}
}
