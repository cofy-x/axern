package policy

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	runtimeegressv1 "github.com/cofy-x/axern/internal/proto/gen/axern/private/runtime/egress/v1"
	"github.com/cofy-x/axern/lib/go/networkpolicy"
	"github.com/cofy-x/axern/runtime/egressd/internal/dnsforward"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Key struct {
	AllocationID string
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
	mu       sync.Mutex
	store    Store
	executor Executor
	records  map[Key]*runtimeegressv1.PreparedEgressPolicy
	health   healthState
}

func NewManager(store Store) (*Manager, error) {
	return NewManagerWithExecutor(store, unavailableExecutor{})
}

func NewManagerWithExecutor(store Store, executor Executor) (*Manager, error) {
	if executor == nil {
		executor = unavailableExecutor{}
	}
	m := &Manager{store: store, executor: executor, records: map[Key]*runtimeegressv1.PreparedEgressPolicy{}}
	if store == nil {
		return m, nil
	}
	loaded, err := store.Load(context.Background())
	if err != nil {
		return nil, err
	}
	ips := map[string]Key{}
	for _, item := range loaded {
		normalized, err := validateStored(item)
		if err != nil {
			return nil, err
		}
		key := keyOf(normalized)
		if _, ok := m.records[key]; ok {
			return nil, fmt.Errorf("duplicate persisted egress policy for allocation %q", key.AllocationID)
		}
		if existing, ok := ips[normalized.GetSandboxIp()]; ok {
			return nil, fmt.Errorf("persisted sandbox IP %s is shared by allocations %q and %q", normalized.GetSandboxIp(), existing.AllocationID, key.AllocationID)
		}
		m.records[key] = normalized
		ips[normalized.GetSandboxIp()] = key
	}
	if err := m.executor.Reconcile(context.Background(), sortedRecords(m.records, "")); err != nil {
		return nil, fmt.Errorf("restore egress enforcement: %w", err)
	}
	return m, nil
}

func (m *Manager) Prepare(ctx context.Context, allocationID string, sandboxIP string, input *commonv1.NetworkEgressPolicy, upstreamSets ...[]string) (*runtimeegressv1.PreparedEgressPolicy, bool, error) {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return nil, false, fmt.Errorf("allocation_id is required")
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
	record := &runtimeegressv1.PreparedEgressPolicy{
		AllocationID: allocationID,
		SandboxIp:    ip.String(),
		Policy:       network.GetEgressPolicy(),
	}
	if len(upstreamSets) > 0 {
		record.UpstreamNameservers = append([]string(nil), upstreamSets[0]...)
	}
	if networkpolicy.RequiresDNSUpstreams(record.GetPolicy()) {
		if _, err := dnsforward.ParseUpstreams(record.GetUpstreamNameservers()); err != nil {
			return nil, false, fmt.Errorf("DNS forwarding upstreams: %w", err)
		}
	} else {
		record.UpstreamNameservers = nil
	}
	key := keyOf(record)

	m.mu.Lock()
	defer m.mu.Unlock()
	if existing, ok := m.records[key]; ok {
		if equivalent(existing, record) {
			return cloneRecord(existing), true, nil
		}
		return nil, false, fmt.Errorf("allocation %q is already prepared with different content", allocationID)
	}
	for existingKey, existing := range m.records {
		if existing.GetSandboxIp() == record.GetSandboxIp() {
			return nil, false, fmt.Errorf("sandbox_ip %s is already owned by allocation %q", record.GetSandboxIp(), existingKey.AllocationID)
		}
	}
	next := cloneMap(m.records)
	next[key] = cloneRecord(record)
	if err := m.executor.Reconcile(ctx, sortedRecords(next, "")); err != nil {
		return nil, false, fmt.Errorf("apply egress enforcement: %w", err)
	}
	if err := m.saveLocked(ctx, next); err != nil {
		if rollbackErr := m.executor.Reconcile(context.Background(), sortedRecords(m.records, "")); rollbackErr != nil {
			return nil, false, errors.Join(err, fmt.Errorf("rollback egress enforcement: %w", rollbackErr))
		}
		return nil, false, err
	}
	m.records = next
	return cloneRecord(record), false, nil
}

func (m *Manager) Delete(ctx context.Context, allocationID string) (bool, error) {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return false, fmt.Errorf("allocation_id is required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key := Key{AllocationID: allocationID}
	if _, ok := m.records[key]; !ok {
		return false, nil
	}
	next := cloneMap(m.records)
	delete(next, key)
	if err := m.executor.Reconcile(ctx, sortedRecords(next, "")); err != nil {
		return false, fmt.Errorf("remove egress enforcement: %w", err)
	}
	if err := m.saveLocked(ctx, next); err != nil {
		if rollbackErr := m.executor.Reconcile(context.Background(), sortedRecords(m.records, "")); rollbackErr != nil {
			return false, errors.Join(err, fmt.Errorf("rollback egress enforcement: %w", rollbackErr))
		}
		return false, err
	}
	m.records = next
	return true, nil
}

func (m *Manager) Get(allocationID string) (*runtimeegressv1.PreparedEgressPolicy, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	record, ok := m.records[Key{AllocationID: strings.TrimSpace(allocationID)}]
	return cloneRecord(record), ok
}

func (m *Manager) List(allocationID string) []*runtimeegressv1.PreparedEgressPolicy {
	m.mu.Lock()
	defer m.mu.Unlock()
	allocationID = strings.TrimSpace(allocationID)
	return sortedRecords(m.records, allocationID)
}

func (m *Manager) Reconcile(ctx context.Context, allocationIDs []string) (result ReconcileResult, err error) {
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
	active := map[Key]struct{}{}
	for _, allocationID := range allocationIDs {
		allocationID = strings.TrimSpace(allocationID)
		if allocationID == "" {
			result.InvalidActivePolicyCount++
			continue
		}
		key := Key{AllocationID: allocationID}
		if _, duplicated := active[key]; duplicated {
			result.InvalidActivePolicyCount++
			continue
		}
		active[key] = struct{}{}
	}
	result.ActivePolicyCount = len(active)
	m.mu.Lock()
	defer m.mu.Unlock()
	next := cloneMap(m.records)
	deletedCount := 0
	for key := range m.records {
		if _, ok := active[key]; ok {
			result.RetainedCount++
			continue
		}
		result.StalePolicyCount++
		delete(next, key)
		deletedCount++
	}
	if deletedCount > 0 {
		if err := m.executor.Reconcile(ctx, sortedRecords(next, "")); err != nil {
			return result, fmt.Errorf("reconcile egress enforcement: %w", err)
		}
		if err := m.saveLocked(ctx, next); err != nil {
			if rollbackErr := m.executor.Reconcile(context.Background(), sortedRecords(m.records, "")); rollbackErr != nil {
				return result, errors.Join(err, fmt.Errorf("rollback egress enforcement: %w", rollbackErr))
			}
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
	enforcement := m.executor.Health(context.Background())
	health.DnsPolicySelfTestOk = enforcement.DNSPolicyReady
	health.StrictEgressSelfTestOk = enforcement.StrictEgressReady
	health.EnforcementRevision = enforcement.Revision
	if !m.health.lastReconcileAt.IsZero() {
		health.LastReconcileAt = timestamppb.New(m.health.lastReconcileAt)
	}
	if health.GetLastReconcileError() != "" || !enforcement.DNSPolicyReady || !enforcement.StrictEgressReady {
		health.Status = runtimeegressv1.EgressManagerStatus_EGRESS_MANAGER_STATUS_ERROR
		if health.GetLastReconcileError() == "" {
			health.LastReconcileError = enforcement.Reason
		}
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
	manager := &Manager{executor: unavailableExecutor{}, records: map[Key]*runtimeegressv1.PreparedEgressPolicy{}}
	prepared, _, err := manager.Prepare(context.Background(), record.GetAllocationID(), record.GetSandboxIp(), record.GetPolicy(), record.GetUpstreamNameservers())
	if err != nil {
		return nil, fmt.Errorf("invalid persisted egress policy: %w", err)
	}
	return prepared, nil
}

func equivalent(left, right *runtimeegressv1.PreparedEgressPolicy) bool {
	return left.GetSandboxIp() == right.GetSandboxIp() && proto.Equal(left.GetPolicy(), right.GetPolicy()) && slices.Equal(left.GetUpstreamNameservers(), right.GetUpstreamNameservers())
}

func keyOf(record *runtimeegressv1.PreparedEgressPolicy) Key {
	return Key{AllocationID: record.GetAllocationID()}
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
	sort.Slice(out, func(i, j int) bool { return out[i].GetAllocationID() < out[j].GetAllocationID() })
	return out
}
