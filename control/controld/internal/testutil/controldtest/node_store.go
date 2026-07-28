package controldtest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type MemoryNodeStore struct {
	mu         sync.Mutex
	records    map[string]*nodekernel.Record
	tokenHashs map[string]string
}

func NewMemoryNodeStore() *MemoryNodeStore {
	return &MemoryNodeStore{records: map[string]*nodekernel.Record{}, tokenHashs: map[string]string{}}
}

func (s *MemoryNodeStore) Register(ctx context.Context, params nodekernel.RegisterParams) (*nodekernel.Record, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkOrSetTokenLocked(params.NodeID, params.NodeAuthToken); err != nil {
		return nil, err
	}
	record := s.records[params.NodeID]
	if record == nil {
		record = &nodekernel.Record{NodeID: params.NodeID, Lifecycle: nodekernel.LifecycleActive, RegisteredAt: params.Now}
		s.records[params.NodeID] = record
	}
	record.NodeTarget = params.NodeTarget
	record.Runtimes = append([]string(nil), params.Runtimes...)
	record.UpdatedAt = params.Now
	return cloneNodeRecord(record), nil
}

func (s *MemoryNodeStore) Report(ctx context.Context, params nodekernel.ReportParams) (*nodekernel.Record, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.checkOrSetTokenLocked(params.NodeID, params.NodeAuthToken); err != nil {
		return nil, err
	}
	record := s.records[params.NodeID]
	if record == nil {
		record = &nodekernel.Record{NodeID: params.NodeID, Lifecycle: nodekernel.LifecycleActive, RegisteredAt: params.Now}
		s.records[params.NodeID] = record
	}
	record.NodeTarget = params.NodeTarget
	record.Runtimes = append([]string(nil), params.Runtimes...)
	record.UpdatedAt = params.Now
	record.Summary = cloneNodeSummary(params.Summary)
	return cloneNodeRecord(record), nil
}

func (s *MemoryNodeStore) Authenticate(ctx context.Context, nodeID, nodeAuthToken string) error {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.tokenHashs[nodeID] == "" || nodeAuthToken == "" {
		return grpcstatus.Error(codes.PermissionDenied, "node auth token is required")
	}
	if s.tokenHashs[nodeID] != hashNodeAuthToken(nodeAuthToken) {
		return grpcstatus.Error(codes.PermissionDenied, "invalid node auth token")
	}
	return nil
}

func (s *MemoryNodeStore) checkOrSetTokenLocked(nodeID, nodeAuthToken string) error {
	if nodeAuthToken == "" {
		return grpcstatus.Error(codes.PermissionDenied, "node auth token is required")
	}
	if s.tokenHashs[nodeID] == "" {
		s.tokenHashs[nodeID] = hashNodeAuthToken(nodeAuthToken)
		return nil
	}
	if s.tokenHashs[nodeID] != hashNodeAuthToken(nodeAuthToken) {
		return grpcstatus.Error(codes.PermissionDenied, "invalid node auth token")
	}
	return nil
}

func (s *MemoryNodeStore) Load(ctx context.Context) ([]*nodekernel.Record, error) {
	_ = ctx
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*nodekernel.Record, 0, len(s.records))
	for _, record := range s.records {
		out = append(out, cloneNodeRecord(record))
	}
	return out, nil
}

func hashNodeAuthToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func ReadySummary(collectedAt time.Time) *nodev1.NodeSummary {
	return &nodev1.NodeSummary{
		CollectedAt:  timestamppb.New(collectedAt),
		NodeState:    nodev1.NodeState_NODE_STATE_READY,
		Capabilities: []string{"feature:ports", "network:bridge"},
		Resources:    &nodev1.ResourcesSummary{AxnodedUsedMilli: 100, AxnodedUsedBytes: 1000},
		Allocatable:  &commonv1.ResourceQuantity{CpuMilli: 8000, MemoryBytes: 16 << 30},
		Capacity:     &commonv1.ResourceQuantity{CpuMilli: 8000, MemoryBytes: 16 << 30},
		Pools:        &nodev1.PoolsSummary{RuntimeSlots: &nodev1.PoolState{Idle: 8, Capacity: 8}, Cgroup: &nodev1.PoolState{Idle: 1, Capacity: 8}, Interface: &nodev1.PoolState{Idle: 1, Capacity: 8}},
		Components: &nodev1.ComponentsSummary{
			Axnoded:  &nodev1.AxnodedSummary{State: nodev1.ComponentState_COMPONENT_STATE_READY, Ready: true},
			Imagemgr: &nodev1.ImagemgrSummary{State: nodev1.ComponentState_COMPONENT_STATE_READY, Reachable: true},
			Imagefsd: &nodev1.ImagefsdSummary{State: nodev1.ComponentState_COMPONENT_STATE_READY, Reachable: true},
			Bpfnet:   &nodev1.BpfNetSummary{State: nodev1.ComponentState_COMPONENT_STATE_READY, Enabled: true, Ready: true},
		},
	}
}

func cloneNodeRecord(in *nodekernel.Record) *nodekernel.Record {
	if in == nil {
		return nil
	}
	return &nodekernel.Record{
		NodeID:        in.NodeID,
		NodeTarget:    in.NodeTarget,
		Runtimes:      append([]string(nil), in.Runtimes...),
		Summary:       cloneNodeSummary(in.Summary),
		Lifecycle:     in.Lifecycle,
		RegisteredAt:  in.RegisteredAt,
		UpdatedAt:     in.UpdatedAt,
		RetiredAt:     in.RetiredAt,
		RetiredReason: in.RetiredReason,
	}
}

func cloneNodeSummary(in *nodev1.NodeSummary) *nodev1.NodeSummary {
	if in == nil {
		return nil
	}
	return proto.Clone(in).(*nodev1.NodeSummary)
}
