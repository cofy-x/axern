package placementkernel

import (
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

// Evaluator is the policy boundary durable admission uses to re-evaluate a
// locked node. Implementations live outside the PostgreSQL adapter.
type Evaluator interface {
	Evaluate(*nodekernel.Record, *Request, time.Time) *Evaluation
}

// AdmissionDecision is the durable result of evaluating a candidate while its
// node row is locked. It keeps the refreshed evaluation and exact evidence
// together so callers cannot accidentally persist a stale preselection.
type AdmissionDecision struct {
	Record                 *nodekernel.Record
	Evaluation             *Evaluation
	Request                *Request
	CapabilityRequirements []*capabilityv1.CapabilityRequirement
}

// Candidate carries a request-specific placement evaluation together with the
// node record that durable admission may lock and refresh.
type Candidate struct {
	*nodekernel.Record
	Evaluation  *Evaluation
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

func EvaluationLess(left, right *Evaluation) bool {
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
	if leftRank.GetRetainedEnvironmentCount() != rightRank.GetRetainedEnvironmentCount() {
		return leftRank.GetRetainedEnvironmentCount() > rightRank.GetRetainedEnvironmentCount()
	}
	if leftRank.GetNydusDaemonAlive() != rightRank.GetNydusDaemonAlive() {
		return leftRank.GetNydusDaemonAlive()
	}
	if leftRank.GetChunkDBRecentAccessAgeSecs() != rightRank.GetChunkDBRecentAccessAgeSecs() {
		return leftRank.GetChunkDBRecentAccessAgeSecs() < rightRank.GetChunkDBRecentAccessAgeSecs()
	}
	if leftRank.GetPeerHealthyCount() != rightRank.GetPeerHealthyCount() {
		return leftRank.GetPeerHealthyCount() > rightRank.GetPeerHealthyCount()
	}
	if leftRank.GetPeerHintedCount() != rightRank.GetPeerHintedCount() {
		return leftRank.GetPeerHintedCount() > rightRank.GetPeerHintedCount()
	}
	if leftRank.GetIdlePoolReady() != rightRank.GetIdlePoolReady() {
		return leftRank.GetIdlePoolReady()
	}
	if leftRank.GetRuntimeSlotOccupancy() != rightRank.GetRuntimeSlotOccupancy() {
		return leftRank.GetRuntimeSlotOccupancy() < rightRank.GetRuntimeSlotOccupancy()
	}
	if leftRank.GetChargedCPUMilli() != rightRank.GetChargedCPUMilli() {
		return leftRank.GetChargedCPUMilli() < rightRank.GetChargedCPUMilli()
	}
	if leftRank.GetChargedMemoryBytes() != rightRank.GetChargedMemoryBytes() {
		return leftRank.GetChargedMemoryBytes() < rightRank.GetChargedMemoryBytes()
	}
	if leftRank.GetChargedEphemeralBytes() != rightRank.GetChargedEphemeralBytes() {
		return leftRank.GetChargedEphemeralBytes() < rightRank.GetChargedEphemeralBytes()
	}
	if left.GetHeartbeatAgeSecs() != right.GetHeartbeatAgeSecs() {
		return left.GetHeartbeatAgeSecs() < right.GetHeartbeatAgeSecs()
	}
	return left.GetNodeID() < right.GetNodeID()
}
