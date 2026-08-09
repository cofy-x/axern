package nodekernel

import (
	"sort"
	"strings"
	"sync"
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/proto"
)

type Registry struct {
	mu    sync.RWMutex
	nodes map[string]*Record
}

type Record struct {
	NodeID        string
	NodeTarget    string
	Runtimes      []string
	Summary       *nodev1.NodeSummary
	Lifecycle     LifecycleStatus
	RegisteredAt  time.Time
	UpdatedAt     time.Time
	RetiredAt     time.Time
	RetiredReason string
	// ReportedCapabilityTransitions contains only transitions committed by the
	// report operation that returned this record. It is transient observability
	// data and is never part of the registry's durable node state.
	ReportedCapabilityTransitions []CapabilityTransition
}

type CapabilityTransition struct {
	Key        *capabilityv1.CapabilityKey
	NewState   capabilityv1.CapabilityState
	ReasonCode capabilityv1.CapabilityReasonCode
}

type LifecycleStatus string

const (
	LifecycleActive  LifecycleStatus = "active"
	LifecycleRetired LifecycleStatus = "retired"
)

func (r *Record) Active() bool {
	return r != nil && r.Lifecycle == LifecycleActive
}

type Snapshot struct {
	Records []*Record
}

func NewRegistry() *Registry {
	return &Registry{
		nodes: make(map[string]*Record),
	}
}

func (r *Registry) Register(nodeID string, nodeTarget string, runtimes []string, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record := r.upsertLocked(nodeID, now)
	record.NodeTarget = strings.TrimSpace(nodeTarget)
	record.Runtimes = normalizeRuntimes(runtimes)
}

func (r *Registry) Report(nodeID string, nodeTarget string, runtimes []string, summary *nodev1.NodeSummary, now time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record := r.upsertLocked(nodeID, now)
	record.NodeTarget = strings.TrimSpace(nodeTarget)
	record.Runtimes = normalizeRuntimes(runtimes)
	record.Summary = CloneNodeSummary(summary)
	record.UpdatedAt = now
}

func (r *Registry) MarkRetired(nodeID string, retiredAt time.Time, reason string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.nodes[nodeID]
	if record == nil {
		return
	}
	record.Lifecycle = LifecycleRetired
	record.RetiredAt = retiredAt
	record.RetiredReason = reason
}

// SyncLifecycle applies the persistent lifecycle state without replacing fresher
// process-local heartbeat and summary data. It lets every controld replica
// converge after an administrative retirement.
func (r *Registry) SyncLifecycle(nodeID string, lifecycle LifecycleStatus, retiredAt time.Time, reason string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	record := r.nodes[nodeID]
	if record == nil {
		return
	}
	if record.Lifecycle == LifecycleRetired && lifecycle != LifecycleRetired {
		return
	}
	record.Lifecycle = lifecycle
	record.RetiredAt = retiredAt
	record.RetiredReason = reason
}

func (r *Registry) Get(nodeID string) (*Record, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	record, ok := r.nodes[nodeID]
	if !ok || record == nil {
		return nil, false
	}
	return cloneRecord(record), true
}

func (r *Registry) Replace(records []*Record) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.nodes = make(map[string]*Record, len(records))
	for _, record := range records {
		if record == nil {
			continue
		}
		r.nodes[record.NodeID] = cloneRecord(record)
	}
}

func (r *Registry) Snapshot() Snapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := Snapshot{
		Records: make([]*Record, 0, len(r.nodes)),
	}
	for _, record := range r.nodes {
		if record == nil {
			continue
		}
		out.Records = append(out.Records, cloneRecord(record))
	}
	sort.Slice(out.Records, func(i, j int) bool {
		return out.Records[i].NodeID < out.Records[j].NodeID
	})
	return out
}

func (r *Registry) DebugNodes(now time.Time, heartbeatWindow, summaryWindow time.Duration) []DebugNode {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]DebugNode, 0, len(r.nodes))
	for _, record := range r.nodes {
		if record == nil {
			continue
		}
		heartbeatFresh := HeartbeatFresh(record.UpdatedAt, now, heartbeatWindow)
		summaryFresh := SummaryFresh(record.Summary, now, summaryWindow)
		freshnessState := ClassifyFreshnessState(heartbeatFresh, summaryFresh)
		if !record.Active() {
			heartbeatFresh = false
			summaryFresh = false
			freshnessState = "retired"
		}
		out = append(out, DebugNode{
			NodeID:           record.NodeID,
			NodeTarget:       record.NodeTarget,
			Runtimes:         append([]string(nil), record.Runtimes...),
			Fresh:            heartbeatFresh && summaryFresh,
			HeartbeatFresh:   heartbeatFresh,
			SummaryFresh:     summaryFresh,
			Lifecycle:        record.Lifecycle,
			FreshnessState:   freshnessState,
			HeartbeatAgeSecs: HeartbeatAgeSecs(record.UpdatedAt, now),
			SummaryAgeSecs:   SummaryAgeSecs(record.Summary, now),
			RegisteredAt:     record.RegisteredAt,
			UpdatedAt:        record.UpdatedAt,
			CollectedAt:      SummaryCollectedAt(record.Summary),
			RetiredAt:        record.RetiredAt,
			RetiredReason:    record.RetiredReason,
			Summary:          CloneNodeSummary(record.Summary),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].NodeID < out[j].NodeID
	})
	return out
}

func (r *Registry) upsertLocked(nodeID string, now time.Time) *Record {
	record, ok := r.nodes[nodeID]
	if !ok {
		record = &Record{
			NodeID:       nodeID,
			Lifecycle:    LifecycleActive,
			RegisteredAt: now,
			UpdatedAt:    now,
		}
		r.nodes[nodeID] = record
		return record
	}
	if record.RegisteredAt.IsZero() {
		record.RegisteredAt = now
	}
	return record
}

func normalizeRuntimes(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, name := range in {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func cloneRecord(in *Record) *Record {
	if in == nil {
		return nil
	}
	return &Record{
		NodeID:        in.NodeID,
		NodeTarget:    in.NodeTarget,
		Runtimes:      append([]string(nil), in.Runtimes...),
		Summary:       CloneNodeSummary(in.Summary),
		Lifecycle:     in.Lifecycle,
		RegisteredAt:  in.RegisteredAt,
		UpdatedAt:     in.UpdatedAt,
		RetiredAt:     in.RetiredAt,
		RetiredReason: in.RetiredReason,
	}
}

func CloneNodeSummary(in *nodev1.NodeSummary) *nodev1.NodeSummary {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*nodev1.NodeSummary)
}
