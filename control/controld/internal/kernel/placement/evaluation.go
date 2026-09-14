package placementkernel

import nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/control/node/v1"

type CandidateState uint8

const (
	CandidateStateUnspecified CandidateState = iota
	CandidateStateEligible
	CandidateStateRejected
)

type RejectionReason uint8

const (
	RejectionReasonUnspecified RejectionReason = iota
	RejectionReasonStaleHeartbeat
	RejectionReasonStaleSummary
	RejectionReasonAxnodedNotReady
	RejectionReasonImagemgrUnavailable
	RejectionReasonImagefsdUnavailable
	RejectionReasonNodeDraining
	RejectionReasonNodeDisabled
	RejectionReasonNodeSelectorMismatch
	RejectionReasonInsufficientCPU
	RejectionReasonInsufficientMemory
	RejectionReasonPortsUnsupported
	RejectionReasonNetworkUnsupported
	RejectionReasonCapabilityUnsupported
	RejectionReasonNodeRetired
	RejectionReasonInsufficientEphemeralStorage
	RejectionReasonNodeMemorySystemReserveExhausted
	RejectionReasonNodeMemoryBudgetUnavailable
)

func (r RejectionReason) String() string {
	if int(r) < len(rejectionReasonNames) {
		return rejectionReasonNames[r]
	}
	return "unspecified"
}

var rejectionReasonNames = [...]string{
	"unspecified",
	"stale_heartbeat",
	"stale_summary",
	"axnoded_not_ready",
	"imagemgr_unavailable",
	"imagefsd_unavailable",
	"node_draining",
	"node_disabled",
	"node_selector_mismatch",
	"insufficient_cpu",
	"insufficient_memory",
	"ports_unsupported",
	"network_unsupported",
	"capability_unsupported",
	"node_retired",
	"insufficient_ephemeral_storage",
	"node_memory_system_reserve_exhausted",
	"node_memory_budget_unavailable",
}

type Rank struct {
	MountedMatch               bool
	RetainedRootfsCount        int32
	RetainedEnvironmentCount   int32
	NydusDaemonAlive           bool
	ChunkDBRecentAccessAgeSecs int64
	PeerHealthyCount           int64
	PeerHintedCount            int64
	BPFNetPreferred            bool
	IdlePoolReady              bool
	AxnodedUsedMilli           int64
	AxnodedUsedBytes           int64
	AxnodedActiveInstances     int64
}

type Evaluation struct {
	NodeID           string
	State            CandidateState
	RejectionReasons []RejectionReason
	HeartbeatAgeSecs int64
	SummaryAgeSecs   int64
	Pools            *nodev1.PoolsSummary
	Resources        *nodev1.ResourcesSummary
	Locality         *nodev1.LocalitySummary
	Rank             *Rank
}

func CloneEvaluation(in *Evaluation) *Evaluation {
	if in == nil {
		return &Evaluation{}
	}
	out := *in
	out.RejectionReasons = append([]RejectionReason(nil), in.RejectionReasons...)
	if in.Rank != nil {
		rank := *in.Rank
		out.Rank = &rank
	}
	return &out
}

func (e *Evaluation) GetNodeID() string {
	if e == nil {
		return ""
	}
	return e.NodeID
}
func (e *Evaluation) GetState() CandidateState {
	if e == nil {
		return CandidateStateUnspecified
	}
	return e.State
}
func (e *Evaluation) GetRejectionReasons() []RejectionReason {
	if e == nil {
		return nil
	}
	return e.RejectionReasons
}
func (e *Evaluation) GetHeartbeatAgeSecs() int64 {
	if e == nil {
		return 0
	}
	return e.HeartbeatAgeSecs
}
func (e *Evaluation) GetRank() *Rank {
	if e == nil || e.Rank == nil {
		return &Rank{}
	}
	return e.Rank
}

func (r *Rank) GetMountedMatch() bool { return r != nil && r.MountedMatch }
func (r *Rank) GetRetainedRootfsCount() int32 {
	if r == nil {
		return 0
	}
	return r.RetainedRootfsCount
}
func (r *Rank) GetRetainedEnvironmentCount() int32 {
	if r == nil {
		return 0
	}
	return r.RetainedEnvironmentCount
}
func (r *Rank) GetNydusDaemonAlive() bool { return r != nil && r.NydusDaemonAlive }
func (r *Rank) GetChunkDBRecentAccessAgeSecs() int64 {
	if r == nil {
		return 0
	}
	return r.ChunkDBRecentAccessAgeSecs
}
func (r *Rank) GetPeerHealthyCount() int64 {
	if r == nil {
		return 0
	}
	return r.PeerHealthyCount
}
func (r *Rank) GetPeerHintedCount() int64 {
	if r == nil {
		return 0
	}
	return r.PeerHintedCount
}
func (r *Rank) GetBPFNetPreferred() bool { return r != nil && r.BPFNetPreferred }
func (r *Rank) GetIdlePoolReady() bool   { return r != nil && r.IdlePoolReady }
func (r *Rank) GetAxnodedActiveInstances() int64 {
	if r == nil {
		return 0
	}
	return r.AxnodedActiveInstances
}
func (r *Rank) GetAxnodedUsedMilli() int64 {
	if r == nil {
		return 0
	}
	return r.AxnodedUsedMilli
}
func (r *Rank) GetAxnodedUsedBytes() int64 {
	if r == nil {
		return 0
	}
	return r.AxnodedUsedBytes
}
