package placementkernel

import (
	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

// Candidate carries a request-specific placement evaluation together with the
// node record that durable admission may lock and refresh.
type Candidate struct {
	*nodekernel.Record
	Evaluation *nodev1.PlacementCandidate
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
