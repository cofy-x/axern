package placementkernel

import (
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

// Evaluator is the policy boundary durable admission uses to re-evaluate a
// locked node. Implementations live outside the PostgreSQL adapter.
type Evaluator interface {
	Evaluate(*nodekernel.Record, *Request, time.Time) *nodev1.PlacementCandidate
}

// AdmissionDecision is the durable result of evaluating a candidate while its
// node row is locked. It keeps the refreshed evaluation and exact evidence
// together so callers cannot accidentally persist a stale preselection.
type AdmissionDecision struct {
	Record                 *nodekernel.Record
	Evaluation             *nodev1.PlacementCandidate
	Request                *Request
	CapabilityDependencies []*capabilityv1.CapabilityDependency
}

// Candidate carries a request-specific placement evaluation together with the
// node record that durable admission may lock and refresh.
type Candidate struct {
	*nodekernel.Record
	Evaluation  *nodev1.PlacementCandidate
	BaseRequest *Request
	Request     *Request
}

func CandidateLess(left, right *Candidate) bool {
	if left == nil {
		return false
	}
	if right == nil {
		return true
	}
	return EvaluationLess(left.Evaluation, right.Evaluation)
}

func EvaluationLess(left, right *nodev1.PlacementCandidate) bool {
	if left == nil {
		return false
	}
	if right == nil {
		return true
	}
	leftRank := left.GetRank()
	rightRank := right.GetRank()

	if leftRank.GetMountedMatch() != rightRank.GetMountedMatch() {
		return leftRank.GetMountedMatch()
	}
	if leftRank.GetRetainedRootfsCount() != rightRank.GetRetainedRootfsCount() {
		return leftRank.GetRetainedRootfsCount() > rightRank.GetRetainedRootfsCount()
	}
	if leftRank.GetRetainedRuntimeCount() != rightRank.GetRetainedRuntimeCount() {
		return leftRank.GetRetainedRuntimeCount() > rightRank.GetRetainedRuntimeCount()
	}
	if leftRank.GetNydusDaemonAlive() != rightRank.GetNydusDaemonAlive() {
		return leftRank.GetNydusDaemonAlive()
	}
	if leftRank.GetChunkdbRecentAccessAgeSecs() != rightRank.GetChunkdbRecentAccessAgeSecs() {
		return leftRank.GetChunkdbRecentAccessAgeSecs() < rightRank.GetChunkdbRecentAccessAgeSecs()
	}
	if leftRank.GetPeerHealthyCount() != rightRank.GetPeerHealthyCount() {
		return leftRank.GetPeerHealthyCount() > rightRank.GetPeerHealthyCount()
	}
	if leftRank.GetPeerHintedCount() != rightRank.GetPeerHintedCount() {
		return leftRank.GetPeerHintedCount() > rightRank.GetPeerHintedCount()
	}
	if leftRank.GetBpfnetPreferred() != rightRank.GetBpfnetPreferred() {
		return leftRank.GetBpfnetPreferred()
	}
	if leftRank.GetIdlePoolReady() != rightRank.GetIdlePoolReady() {
		return leftRank.GetIdlePoolReady()
	}
	if leftRank.GetAxnodedActiveInstances() != rightRank.GetAxnodedActiveInstances() {
		return leftRank.GetAxnodedActiveInstances() < rightRank.GetAxnodedActiveInstances()
	}
	if leftRank.GetAxnodedUsedMilli() != rightRank.GetAxnodedUsedMilli() {
		return leftRank.GetAxnodedUsedMilli() < rightRank.GetAxnodedUsedMilli()
	}
	if leftRank.GetAxnodedUsedBytes() != rightRank.GetAxnodedUsedBytes() {
		return leftRank.GetAxnodedUsedBytes() < rightRank.GetAxnodedUsedBytes()
	}
	if left.GetHeartbeatAgeSecs() != right.GetHeartbeatAgeSecs() {
		return left.GetHeartbeatAgeSecs() < right.GetHeartbeatAgeSecs()
	}
	return left.GetNodeID() < right.GetNodeID()
}
