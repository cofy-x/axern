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
	record, already, err := mustManager.Prepare(context.Background(), " alloc-1 ", "10.0.0.8", dnsDeny("BÜCHER.Example.", "xn--bcher-kva.example"), testDNSUpstreams)
	if err != nil {
		t.Fatal(err)
	}
	if already {
		t.Fatal("first prepare reported already prepared")
	}
	if got := record.GetPolicy().GetDnsDeny().GetDeniedDomains(); len(got) != 1 || got[0] != "xn--bcher-kva.example" {
		t.Fatalf("unexpected normalized domains: %v", got)
	}
	retry, already, err := mustManager.Prepare(context.Background(), "alloc-1", "10.0.0.8", dnsDeny("xn--bcher-kva.example"), testDNSUpstreams)
	if err != nil || !already {
		t.Fatalf("idempotent prepare = (%v, %v), want success/already", err, already)
	}
	if !proto.Equal(retry, record) {
		t.Fatal("idempotent retry changed the durable record")
	}
}

func TestPrepareFencesContentAndIPReuse(t *testing.T) {
	m := newTestManager(t, nil)
	prepare(t, m, "alloc-1", "10.0.0.8", 1, dnsDeny("example.com"))

	if _, _, err := m.Prepare(context.Background(), "alloc-1", "10.0.0.9", dnsDeny("example.com"), testDNSUpstreams); err == nil || !strings.Contains(err.Error(), "different content") {
		t.Fatalf("allocation content drift error = %v", err)
	}
	if _, _, err := m.Prepare(context.Background(), "alloc-2", "10.0.0.8", dnsDeny("other.example"), testDNSUpstreams); err == nil || !strings.Contains(err.Error(), "already owned") {
		t.Fatalf("IP reuse error = %v", err)
	}
}

func TestPrepareRejectsUnsafeSandboxIPs(t *testing.T) {
	m := newTestManager(t, nil)
	for _, ip := range []string{"127.0.0.1", "169.254.1.1", "::1", "fe80::1", "not-an-ip"} {
		if _, _, err := m.Prepare(context.Background(), "alloc", ip, dnsDeny("example.com")); err == nil {
			t.Fatalf("Prepare accepted unsafe sandbox IP %q", ip)
		}
	}
}

func TestPrepareRequiresUpstreamsOnlyForDNSForwardingPolicies(t *testing.T) {
	m := newTestManager(t, nil)
	if _, _, err := m.Prepare(context.Background(), "dns", "10.0.0.8", dnsDeny("example.com")); err == nil || !strings.Contains(err.Error(), "upstream") {
		t.Fatalf("DNS forwarding policy without upstream error = %v", err)
	}
	if _, _, err := m.Prepare(context.Background(), "loopback", "10.0.0.8", dnsDeny("example.com"), []string{"127.0.0.1"}); err == nil || !strings.Contains(err.Error(), "usable node resolver") {
		t.Fatalf("DNS forwarding policy with loopback upstream error = %v", err)
	}
	if len(m.List("")) != 0 {
		t.Fatal("rejected DNS forwarding policy changed manager state")
	}
	denyAll := &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_Strict{Strict: &commonv1.StrictEgressPolicy{}}}
	record, _, err := m.Prepare(context.Background(), "deny-all", "10.0.0.9", denyAll, []string{"invalid"})
	if err != nil {
		t.Fatalf("strict deny-all unexpectedly depended on DNS: %v", err)
	}
	if len(record.GetUpstreamNameservers()) != 0 {
		t.Fatalf("strict deny-all persisted unused upstreams: %v", record.GetUpstreamNameservers())
	}
}

func TestPrepareCanonicalizesMappedIPv4BeforeIPFencing(t *testing.T) {
	m := newTestManager(t, nil)
	record := prepare(t, m, "alloc-1", "::ffff:10.0.0.8", 1, dnsDeny("example.com"))
	if record.GetSandboxIp() != "10.0.0.8" {
		t.Fatalf("sandbox IP = %q, want canonical IPv4", record.GetSandboxIp())
	}
	if _, _, err := m.Prepare(context.Background(), "alloc-2", "10.0.0.8", dnsDeny("example.net"), testDNSUpstreams); err == nil {
		t.Fatal("canonical IP collision was accepted")
	}
}

func TestPersistenceRecoveryAndAtomicMutation(t *testing.T) {
	root := t.TempDir()
	store := NewJSONStore(root)
	m := newTestManager(t, store)
	record := prepare(t, m, "alloc-1", "10.0.0.8", 9, dnsDeny("example.com"))

	restarted := newTestManager(t, store)
	recovered, ok := restarted.Get("alloc-1")
	if !ok {
		t.Fatalf("recovered record = %#v, %v", recovered, ok)
	}
	if !proto.Equal(recovered, record) {
		t.Fatal("recovery did not preserve the authoritative record")
	}

	failing := &memoryStore{records: restarted.List("")}
	failedManager := newTestManager(t, failing)
	failing.saveErr = errors.New("disk full")
	if deleted, err := failedManager.Delete(context.Background(), "alloc-1"); err == nil || deleted {
		t.Fatalf("delete with failed persistence = (%v, %v)", deleted, err)
	}
	if _, ok := failedManager.Get("alloc-1"); !ok {
		t.Fatal("failed persistence changed in-memory state")
	}
}

func TestReconcileUsesAllocationOwnershipAndCollectsOrphans(t *testing.T) {
	m := newTestManager(t, nil)
	prepare(t, m, "alloc-1", "10.0.0.8", 4, dnsDeny("one.example"))
	prepare(t, m, "alloc-2", "10.0.0.9", 5, dnsDeny("two.example"))

	result, err := m.Reconcile(context.Background(), []string{"alloc-1", ""})
	if err != nil {
		t.Fatal(err)
	}
	if result.ActivePolicyCount != 1 || result.RetainedCount != 1 || result.DeletedCount != 1 || result.StalePolicyCount != 1 || result.InvalidActivePolicyCount != 1 {
		t.Fatalf("unexpected reconcile result: %+v", result)
	}
	if _, ok := m.Get("alloc-1"); !ok {
		t.Fatal("owned record was not retained")
	}
	if _, ok := m.Get("alloc-2"); ok {
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
	prepare(t, m, "alloc-1", "10.0.0.8", 1, dnsDeny("example.com"))
	store.saveErr = errors.New("disk full")
	if _, err := m.Reconcile(context.Background(), nil); err == nil {
		t.Fatal("Reconcile succeeded despite persistence failure")
	}
	if _, ok := m.Get("alloc-1"); !ok {
		t.Fatal("failed reconcile changed in-memory state")
	}
	if m.Health().GetStatus() != runtimeegressv1.EgressManagerStatus_EGRESS_MANAGER_STATUS_ERROR {
		t.Fatal("failed reconcile was not reflected in health")
	}
}

func TestPersistenceFailureRestoresPreviousDataplane(t *testing.T) {
	store := &memoryStore{}
	executor := &recordingExecutor{}
	m, err := NewManagerWithExecutor(store, executor)
	if err != nil {
		t.Fatal(err)
	}
	store.saveErr = errors.New("disk full")
	if _, _, err := m.Prepare(context.Background(), "alloc-1", "10.0.0.8", dnsDeny("example.com"), testDNSUpstreams); err == nil {
		t.Fatal("Prepare succeeded despite persistence failure")
	}
	if len(executor.generations) != 3 || len(executor.generations[0]) != 0 || len(executor.generations[1]) != 1 || len(executor.generations[2]) != 0 {
		t.Fatalf("dataplane generations = %#v, want empty/apply/rollback", executor.generations)
	}
	if len(m.List("")) != 0 {
		t.Fatal("failed persistence published policy state")
	}
}

type recordingExecutor struct {
	generations [][]*runtimeegressv1.PreparedEgressPolicy
}

func (e *recordingExecutor) Reconcile(_ context.Context, records []*runtimeegressv1.PreparedEgressPolicy) error {
	e.generations = append(e.generations, cloneRecords(records))
	return nil
}
func (e *recordingExecutor) Health(context.Context) EnforcementHealth {
	return EnforcementHealth{DNSPolicyReady: true, StrictEgressReady: true, Revision: int64(len(e.generations))}
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

func prepare(t *testing.T, m *Manager, allocationID string, ip string, revision int64, input *commonv1.NetworkEgressPolicy) *runtimeegressv1.PreparedEgressPolicy {
	t.Helper()
	record, _, err := m.Prepare(context.Background(), allocationID, ip, input, testDNSUpstreams)
	if err != nil {
		t.Fatal(err)
	}
	return record
}

func dnsDeny(domains ...string) *commonv1.NetworkEgressPolicy {
	return &commonv1.NetworkEgressPolicy{Policy: &commonv1.NetworkEgressPolicy_DnsDeny{DnsDeny: &commonv1.DnsDenyPolicy{DeniedDomains: domains}}}
}

var testDNSUpstreams = []string{"192.0.2.53"}

func cloneRecords(records []*runtimeegressv1.PreparedEgressPolicy) []*runtimeegressv1.PreparedEgressPolicy {
	out := make([]*runtimeegressv1.PreparedEgressPolicy, 0, len(records))
	for _, record := range records {
		out = append(out, proto.Clone(record).(*runtimeegressv1.PreparedEgressPolicy))
	}
	return out
}
