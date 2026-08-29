package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cofy-x/axern/lib/go/networkpolicy"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Key struct {
	AllocationID string
	Attempt      int64
}

type ReconcileResult struct {
	ActivePolicyCount        int
	RetainedCount            int
	DeletedCount             int
	StalePolicyCount         int
	InvalidActivePolicyCount int
}

type healthState struct {
	lastReconcileAt    time.Time
	lastReconcileError string
	lastResult         ReconcileResult
}

type Manager struct {
	mu      sync.Mutex
	store   Store
	records map[Key]*runtimeegressv1.PreparedEgressPolicy
	health  healthState
}

func NewManager(store Store) (*Manager, error) {
	m := &Manager{store: store, records: map[Key]*runtimeegressv1.PreparedEgressPolicy{}}
	if store == nil {
		return m, nil
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		return nil, err
	}
	ips := map[string]Key{}
	allocations := map[string]Key{}
	for _, item := range loaded {
		normalized, err := validateStored(item)
		if err != nil {
			return nil, err
		}
		key := keyOf(normalized)
		if _, ok := m.records[key]; ok {
			return nil, fmt.Errorf("duplicate persisted egress policy for allocation %q attempt %d", key.AllocationID, key.Attempt)
		}
		if existing, ok := allocations[key.AllocationID]; ok {
			return nil, fmt.Errorf("persisted allocation %q has multiple attempts %d and %d", key.AllocationID, existing.Attempt, key.Attempt)
		}
		if existing, ok := ips[normalized.GetSandboxIp()]; ok {
			return nil, fmt.Errorf("persisted sandbox IP %s is shared by %q/%d and %q/%d", normalized.GetSandboxIp(), existing.AllocationID, existing.Attempt, key.AllocationID, key.Attempt)
		}
		normalized.RecoveryState = runtimeegressv1.EgressPolicyRecoveryState_EGRESS_POLICY_RECOVERY_STATE_RECOVERED
		m.records[key] = normalized
		ips[normalized.GetSandboxIp()] = key
		allocations[key.AllocationID] = key
	}
	if len(loaded) > 0 {
		if err := m.saveLocked(context.Background(), m.records); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *Manager) Prepare(ctx context.Context, allocationID string, attempt int64, sandboxIP string, input *commonv1.NetworkEgressPolicy, executionRevision int64) (*runtimeegressv1.PreparedEgressPolicy, bool, error) {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return nil, false, fmt.Errorf("allocation_id is required")
	}
	if attempt <= 0 {
		return nil, false, fmt.Errorf("attempt must be positive")
	}
	if executionRevision <= 0 {
		return nil, false, fmt.Errorf("execution_revision must be positive")
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(sandboxIP))
	if err != nil || !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return nil, false, fmt.Errorf("sandbox_ip must be a valid unicast IP address")
	}
	ip = ip.Unmap()
	if input == nil {
		return nil, false, fmt.Errorf("policy is required")
	}
	network, err := networkpolicy.Normalize(&commonv1.NetworkSpec{EgressPolicy: proto.Clone(input).(*commonv1.NetworkEgressPolicy)})
	if err != nil {
		return nil, false, fmt.Errorf("invalid policy: %w", err)
	}
	if network.GetEgressPolicy() == nil {
		return nil, false, fmt.Errorf("policy is required")
	}
	digest, err := policyDigest(network.GetEgressPolicy())
	if err != nil {
		return nil, false, err
	}
	record := &runtimeegressv1.PreparedEgressPolicy{
		AllocationID:      allocationID,
		Attempt:           attempt,
		SandboxIp:         ip.String(),
		Policy:            network.GetEgressPolicy(),
		PolicyDigest:      digest,
		ExecutionRevision: executionRevision,
		RecoveryState:     runtimeegressv1.EgressPolicyRecoveryState_EGRESS_POLICY_RECOVERY_STATE_APPLIED,
		UpdatedAt:         timestamppb.Now(),
	}
	key := keyOf(record)

	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.records[key]; ok {
		if equivalent(existing, record) {
			return cloneRecord(existing), true, nil
		}
		return nil, false, fmt.Errorf("allocation %q attempt %d is already prepared with different content", allocationID, attempt)
	}
	for existingKey, existing := range m.records {
		if existingKey.AllocationID == allocationID {
			if existingKey.Attempt > attempt {
				return nil, false, fmt.Errorf("allocation %q attempt %d is stale; current attempt is %d", allocationID, attempt, existingKey.Attempt)
			}
			continue
		}
		if existing.GetSandboxIp() == record.GetSandboxIp() {
			return nil, false, fmt.Errorf("sandbox_ip %s is already owned by allocation %q attempt %d", record.GetSandboxIp(), existingKey.AllocationID, existingKey.Attempt)
		}
	}
	next := cloneMap(m.records)
	for existingKey := range next {
		if existingKey.AllocationID == allocationID && existingKey.Attempt < attempt {
			delete(next, existingKey)
		}
	}
	next[key] = cloneRecord(record)
	if err := m.saveLocked(ctx, next); err != nil {
		return nil, false, err
	}
	m.records = next
	return cloneRecord(record), false, nil
}

func (m *Manager) Delete(ctx context.Context, allocationID string, attempt int64) (bool, error) {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" || attempt <= 0 {
		return false, fmt.Errorf("allocation_id and a positive attempt are required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := Key{AllocationID: allocationID, Attempt: attempt}
	if _, ok := m.records[key]; !ok {
		for existingKey := range m.records {
			if existingKey.AllocationID == allocationID && existingKey.Attempt > attempt {
				return false, fmt.Errorf("allocation %q attempt %d is stale; current attempt is %d", allocationID, attempt, existingKey.Attempt)
			}
		}
		return false, nil
	}
	next := cloneMap(m.records)
	delete(next, key)
	if err := m.saveLocked(ctx, next); err != nil {
		return false, err
	}
	m.records = next
	return true, nil
}

func (m *Manager) Get(allocationID string, attempt int64) (*runtimeegressv1.PreparedEgressPolicy, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[Key{AllocationID: strings.TrimSpace(allocationID), Attempt: attempt}]
	return cloneRecord(record), ok
}

func (m *Manager) List(allocationID string) []*runtimeegressv1.PreparedEgressPolicy {
	m.mu.Lock()
	defer m.mu.Unlock()
	allocationID = strings.TrimSpace(allocationID)
	return sortedRecords(m.records, allocationID)
}

func (m *Manager) Reconcile(ctx context.Context, active []*runtimeegressv1.ActiveEgressPolicy) (result ReconcileResult, err error) {
	defer func() {
		m.mu.Lock()
		m.health.lastReconcileAt = time.Now().UTC()
		m.health.lastResult = result
		if err != nil {
			m.health.lastReconcileError = err.Error()
		} else {
			m.health.lastReconcileError = ""
		}
		m.mu.Unlock()
	}()
	proofs := map[Key]*runtimeegressv1.ActiveEgressPolicy{}
	for _, proof := range active {
		if err := validateProof(proof); err != nil {
			result.InvalidActivePolicyCount++
			continue
		}
		key := Key{AllocationID: strings.TrimSpace(proof.GetAllocationID()), Attempt: proof.GetAttempt()}
		if _, duplicated := proofs[key]; duplicated {
			result.InvalidActivePolicyCount++
			continue
		}
		proofs[key] = proof
	}
	result.ActivePolicyCount = len(proofs)
	m.mu.Lock()
	defer m.mu.Unlock()
	next := cloneMap(m.records)
	deletedCount := 0
	for key, record := range m.records {
		proof, ok := proofs[key]
		if ok && proofMatches(record, proof) {
			result.RetainedCount++
			continue
		}
		result.StalePolicyCount++
		delete(next, key)
		deletedCount++
	}
	if deletedCount > 0 {
		if err := m.saveLocked(ctx, next); err != nil {
			return result, err
		}
		m.records = next
		result.DeletedCount = deletedCount
	}
	return result, nil
}

func (m *Manager) Health() *runtimeegressv1.EgressManagerHealth {
	m.mu.Lock()
	defer m.mu.Unlock()
	health := &runtimeegressv1.EgressManagerHealth{
		Status:                                runtimeegressv1.EgressManagerStatus_EGRESS_MANAGER_STATUS_OK,
		PreparedPolicyCount:                   int32(len(m.records)),
		LastReconcileError:                    m.health.lastReconcileError,
		LastReconcileRetainedCount:            int32(m.health.lastResult.RetainedCount),
		LastReconcileDeletedCount:             int32(m.health.lastResult.DeletedCount),
		LastReconcileActivePolicyCount:        int32(m.health.lastResult.ActivePolicyCount),
		LastReconcileStalePolicyCount:         int32(m.health.lastResult.StalePolicyCount),
		LastReconcileInvalidActivePolicyCount: int32(m.health.lastResult.InvalidActivePolicyCount),
	}
	for _, record := range m.records {
		if record.GetRecoveryState() == runtimeegressv1.EgressPolicyRecoveryState_EGRESS_POLICY_RECOVERY_STATE_RECOVERED {
			health.RecoveredPolicyCount++
		}
	}
	if !m.health.lastReconcileAt.IsZero() {
		health.LastReconcileAt = timestamppb.New(m.health.lastReconcileAt)
	}
	if health.GetLastReconcileError() != "" {
		health.Status = runtimeegressv1.EgressManagerStatus_EGRESS_MANAGER_STATUS_ERROR
	}
	return health
}

func (m *Manager) saveLocked(ctx context.Context, records map[Key]*runtimeegressv1.PreparedEgressPolicy) error {
	if m.store == nil {
		return nil
	}
	if err := m.store.Save(ctx, sortedRecords(records, "")); err != nil {
		return fmt.Errorf("persist egress policies: %w", err)
	}
	return nil
}

func validateStored(record *runtimeegressv1.PreparedEgressPolicy) (*runtimeegressv1.PreparedEgressPolicy, error) {
	if record == nil {
		return nil, fmt.Errorf("persisted egress policy is required")
	}
	manager := &Manager{}
	prepared, _, err := manager.Prepare(context.Background(), record.GetAllocationID(), record.GetAttempt(), record.GetSandboxIp(), record.GetPolicy(), record.GetExecutionRevision())
	if err != nil {
		return nil, fmt.Errorf("invalid persisted egress policy: %w", err)
	}
	if record.GetPolicyDigest() != prepared.GetPolicyDigest() {
		return nil, fmt.Errorf("invalid persisted egress policy: policy digest mismatch")
	}
	prepared.UpdatedAt = record.GetUpdatedAt()
	return prepared, nil
}

func validateProof(proof *runtimeegressv1.ActiveEgressPolicy) error {
	if proof == nil || strings.TrimSpace(proof.GetAllocationID()) == "" || proof.GetAttempt() <= 0 || proof.GetExecutionRevision() <= 0 || strings.TrimSpace(proof.GetPolicyDigest()) == "" {
		return fmt.Errorf("active policy proof is incomplete")
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(proof.GetSandboxIp()))
	if err != nil || !ip.IsValid() {
		return fmt.Errorf("active policy proof sandbox IP is invalid")
	}
	return nil
}

func proofMatches(record *runtimeegressv1.PreparedEgressPolicy, proof *runtimeegressv1.ActiveEgressPolicy) bool {
	return record.GetSandboxIp() == strings.TrimSpace(proof.GetSandboxIp()) && record.GetPolicyDigest() == strings.TrimSpace(proof.GetPolicyDigest()) && record.GetExecutionRevision() == proof.GetExecutionRevision()
}

func policyDigest(input *commonv1.NetworkEgressPolicy) (string, error) {
	data, err := proto.MarshalOptions{Deterministic: true}.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("marshal normalized policy: %w", err)
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func equivalent(left, right *runtimeegressv1.PreparedEgressPolicy) bool {
	return left.GetSandboxIp() == right.GetSandboxIp() && left.GetPolicyDigest() == right.GetPolicyDigest() && left.GetExecutionRevision() == right.GetExecutionRevision()
}

func keyOf(record *runtimeegressv1.PreparedEgressPolicy) Key {
	return Key{AllocationID: record.GetAllocationID(), Attempt: record.GetAttempt()}
}

func cloneRecord(record *runtimeegressv1.PreparedEgressPolicy) *runtimeegressv1.PreparedEgressPolicy {
	if record == nil {
		return nil
	}
	return proto.Clone(record).(*runtimeegressv1.PreparedEgressPolicy)
}

func cloneMap(records map[Key]*runtimeegressv1.PreparedEgressPolicy) map[Key]*runtimeegressv1.PreparedEgressPolicy {
	out := make(map[Key]*runtimeegressv1.PreparedEgressPolicy, len(records))
	for key, record := range records {
		out[key] = cloneRecord(record)
	}
	return out
}

func sortedRecords(records map[Key]*runtimeegressv1.PreparedEgressPolicy, allocationID string) []*runtimeegressv1.PreparedEgressPolicy {
	out := make([]*runtimeegressv1.PreparedEgressPolicy, 0, len(records))
	for key, record := range records {
		if allocationID == "" || key.AllocationID == allocationID {
			out = append(out, cloneRecord(record))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].GetAllocationID() == out[j].GetAllocationID() {
			return out[i].GetAttempt() < out[j].GetAttempt()
		}
		return out[i].GetAllocationID() < out[j].GetAllocationID()
	})
	return out
}
