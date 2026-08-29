package policy

import (
	"context"
	"errors"
	"strings"
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
	"google.golang.org/protobuf/proto"
)

func TestPrepareNormalizesAndIsIdempotent(t *testing.T) {
	mustManager := newTestManager(t, nil)
	record, already, err := mustManager.Prepare(context.Background(), " alloc-1 ", 1, "10.0.0.8", dnsDeny("BÜCHER.Example.", "xn--bcher-kva.example"), 7)
	if err != nil {
		t.Fatal(err)
	}
	if already {
		t.Fatal("first prepare reported already prepared")
	}
	if got := record.GetPolicy().GetDnsDeny().GetDeniedDomains(); len(got) != 1 || got[0] != "xn--bcher-kva.example" {
		t.Fatalf("unexpected normalized domains: %v", got)
	}
	if !strings.HasPrefix(record.GetPolicyDigest(), "sha256:") {
		t.Fatalf("unexpected digest: %q", record.GetPolicyDigest())
	}

	retry, already, err := mustManager.Prepare(context.Background(), "alloc-1", 1, "10.0.0.8", dnsDeny("xn--bcher-kva.example"), 7)
	if err != nil || !already {
		t.Fatalf("idempotent prepare = (%v, %v), want success/already", err, already)
	}
	if retry.GetPolicyDigest() != record.GetPolicyDigest() {
		t.Fatalf("digest changed: %q != %q", retry.GetPolicyDigest(), record.GetPolicyDigest())
	}
}

func TestPrepareFencesContentAttemptAndIPReuse(t *testing.T) {
	m := newTestManager(t, nil)
	prepare(t, m, "alloc-1", 2, "10.0.0.8", 1, dnsDeny("example.com"))

	if _, _, err := m.Prepare(context.Background(), "alloc-1", 2, "10.0.0.9", dnsDeny("example.com"), 1); err == nil || !strings.Contains(err.Error(), "different content") {
		t.Fatalf("same-attempt drift error = %v", err)
	}
	if _, _, err := m.Prepare(context.Background(), "alloc-1", 1, "10.0.0.9", dnsDeny("example.com"), 1); err == nil || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale attempt error = %v", err)
	}
	if _, _, err := m.Prepare(context.Background(), "alloc-2", 1, "10.0.0.8", dnsDeny("other.example"), 1); err == nil || !strings.Contains(err.Error(), "already owned") {
		t.Fatalf("IP reuse error = %v", err)
	}

	prepare(t, m, "alloc-1", 3, "10.0.0.8", 2, dnsDeny("new.example"))
	if _, ok := m.Get("alloc-1", 2); ok {
		t.Fatal("superseded attempt remained present")
	}
	if deleted, err := m.Delete(context.Background(), "alloc-1", 2); err == nil || deleted || !strings.Contains(err.Error(), "stale") {
		t.Fatalf("stale delete = (%v, %v)", deleted, err)
	}
}

func TestPrepareRejectsUnsafeSandboxIPs(t *testing.T) {
	m := newTestManager(t, nil)
	for _, ip := range []string{"127.0.0.1", "169.254.1.1", "::1", "fe80::1", "not-an-ip"} {
		if _, _, err := m.Prepare(context.Background(), "alloc", 1, ip, dnsDeny("example.com"), 1); err == nil {
			t.Fatalf("Prepare accepted unsafe sandbox IP %q", ip)
		}
	}
}

func TestPrepareCanonicalizesMappedIPv4BeforeIPFencing(t *testing.T) {
	m := newTestManager(t, nil)
	record := prepare(t, m, "alloc-1", 1, "::ffff:10.0.0.8", 1, dnsDeny("example.com"))
	if record.GetSandboxIp() != "10.0.0.8" {
		t.Fatalf("sandbox IP = %q, want canonical IPv4", record.GetSandboxIp())
	}
	if _, _, err := m.Prepare(context.Background(), "alloc-2", 1, "10.0.0.8", dnsDeny("example.net"), 1); err == nil {
		t.Fatal("canonical IP collision was accepted")
	}
}

func TestPersistenceRecoveryAndAtomicMutation(t *testing.T) {
	root := t.TempDir()
	store := NewJSONStore(root)
	m := newTestManager(t, store)
	record := prepare(t, m, "alloc-1", 1, "10.0.0.8", 9, dnsDeny("example.com"))

	restarted := newTestManager(t, store)
	recovered, ok := restarted.Get("alloc-1", 1)
	if !ok || recovered.GetRecoveryState() != runtimeegressv1.EgressPolicyRecoveryState_EGRESS_POLICY_RECOVERY_STATE_RECOVERED {
		t.Fatalf("recovered record = %#v, %v", recovered, ok)
	}
	if recovered.GetPolicyDigest() != record.GetPolicyDigest() || restarted.Health().GetRecoveredPolicyCount() != 1 {
		t.Fatal("recovery did not preserve digest and health count")
	}

	failing := &memoryStore{records: restarted.List("")}
	failedManager := newTestManager(t, failing)
	failing.saveErr = errors.New("disk full")
	if deleted, err := failedManager.Delete(context.Background(), "alloc-1", 1); err == nil || deleted {
		t.Fatalf("delete with failed persistence = (%v, %v)", deleted, err)
	}
	if _, ok := failedManager.Get("alloc-1", 1); !ok {
		t.Fatal("failed persistence changed in-memory state")
	}
}

func TestReconcileRequiresExactProofAndCollectsOrphans(t *testing.T) {
	m := newTestManager(t, nil)
	one := prepare(t, m, "alloc-1", 1, "10.0.0.8", 4, dnsDeny("one.example"))
	prepare(t, m, "alloc-2", 1, "10.0.0.9", 5, dnsDeny("two.example"))

	result, err := m.Reconcile(context.Background(), []*runtimeegressv1.ActiveEgressPolicy{
		activeProof(one),
		{AllocationID: "alloc-2", Attempt: 1, SandboxIp: "10.0.0.9", PolicyDigest: "sha256:wrong", ExecutionRevision: 5},
		{AllocationID: "", Attempt: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ActivePolicyCount != 2 || result.RetainedCount != 1 || result.DeletedCount != 1 || result.StalePolicyCount != 1 || result.InvalidActivePolicyCount != 1 {
		t.Fatalf("unexpected reconcile result: %+v", result)
	}
	if _, ok := m.Get("alloc-1", 1); !ok {
		t.Fatal("exact record was not retained")
	}
	if _, ok := m.Get("alloc-2", 1); ok {
		t.Fatal("mismatched record was not collected")
	}
	health := m.Health()
	if health.GetLastReconcileRetainedCount() != 1 || health.GetLastReconcileDeletedCount() != 1 || health.GetLastReconcileAt() == nil {
		t.Fatalf("unexpected health: %#v", health)
	}
}

func TestReconcileSaveFailureDoesNotPublishNewState(t *testing.T) {
	store := &memoryStore{}
	m := newTestManager(t, store)
	prepare(t, m, "alloc-1", 1, "10.0.0.8", 1, dnsDeny("example.com"))
	store.saveErr = errors.New("disk full")
	if _, err := m.Reconcile(context.Background(), nil); err == nil {
		t.Fatal("Reconcile succeeded despite persistence failure")
	}
	if _, ok := m.Get("alloc-1", 1); !ok {
		t.Fatal("failed reconcile changed in-memory state")
	}
	if m.Health().GetStatus() != runtimeegressv1.EgressManagerStatus_EGRESS_MANAGER_STATUS_ERROR {
		t.Fatal("failed reconcile was not reflected in health")
	}
}

type memoryStore struct {
	records []*runtimeegressv1.PreparedEgressPolicy
	saveErr error
}

func (s *memoryStore) Load(context.Context) ([]*runtimeegressv1.PreparedEgressPolicy, error) {
	return cloneRecords(s.records), nil
}

func (s *memoryStore) Save(_ context.Context, records []*runtimeegressv1.PreparedEgressPolicy) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	s.records = cloneRecords(records)
	return nil
}

func newTestManager(t *testing.T, store Store) *Manager {
	t.Helper()
	m, err := NewManager(store)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func prepare(t *testing.T, m *Manager, allocationID string, attempt int64, ip string, revision int64, input *commonv1.NetworkEgressPolicy) *runtimeegressv1.PreparedEgressPolicy {
	t.Helper()
	record, _, err := m.Prepare(context.Background(), allocationID, attempt, ip, input, revision)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func dnsDeny(domains ...string) *commonv1.NetworkEgressPolicy {
	return &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: domains}}}
}

func activeProof(record *runtimeegressv1.PreparedEgressPolicy) *runtimeegressv1.ActiveEgressPolicy {
	return &runtimeegressv1.ActiveEgressPolicy{
		AllocationID:      record.GetAllocationID(),
		Attempt:           record.GetAttempt(),
		SandboxIp:         record.GetSandboxIp(),
		PolicyDigest:      record.GetPolicyDigest(),
		ExecutionRevision: record.GetExecutionRevision(),
	}
}

func cloneRecords(records []*runtimeegressv1.PreparedEgressPolicy) []*runtimeegressv1.PreparedEgressPolicy {
	out := make([]*runtimeegressv1.PreparedEgressPolicy, 0, len(records))
	for _, record := range records {
		out = append(out, proto.Clone(record).(*runtimeegressv1.PreparedEgressPolicy))
	}
	return out
}
