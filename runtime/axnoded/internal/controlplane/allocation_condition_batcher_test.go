package controlplane

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestAllocationConditionBatcherCoalescesAttemptAndRevision(t *testing.T) {
	batches := make(chan []*nodev1.AllocationCapabilityConditionReport, 1)
	batcher := newAllocationConditionBatcher(func(_ context.Context, reports []*nodev1.AllocationCapabilityConditionReport) error {
		batches <- reports
		return nil
	})
	batcher.batchDelay = 20 * time.Millisecond
	batcher.Start()
	defer batcher.Stop()

	batcher.Enqueue(conditionReport("allocation-a", 1, 1))
	batcher.Enqueue(conditionReport("allocation-a", 1, 2))
	batcher.Enqueue(conditionReport("allocation-a", 2, 1))
	batcher.Enqueue(conditionReport("allocation-a", 1, 3))

	batch := awaitConditionBatch(t, batches)
	if len(batch) != 1 || batch[0].GetAttempt() != 2 || batch[0].GetConditionSet().GetRevision() != 1 {
		t.Fatalf("batch = %#v, want newest attempt and its revision", batch)
	}
}

func TestAllocationConditionBatcherRetryDoesNotOverwriteNewerRevision(t *testing.T) {
	firstSend := make(chan struct{})
	releaseFirst := make(chan struct{})
	batches := make(chan []*nodev1.AllocationCapabilityConditionReport, 1)
	var mu sync.Mutex
	calls := 0
	batcher := newAllocationConditionBatcher(func(_ context.Context, reports []*nodev1.AllocationCapabilityConditionReport) error {
		mu.Lock()
		calls++
		call := calls
		mu.Unlock()
		if call == 1 {
			close(firstSend)
			<-releaseFirst
			return errors.New("temporary failure")
		}
		batches <- reports
		return nil
	})
	batcher.batchDelay = 0
	batcher.retryInitialDelay = time.Millisecond
	batcher.retryMaxDelay = time.Millisecond
	batcher.jitter = nil
	batcher.Start()
	defer batcher.Stop()

	batcher.Enqueue(conditionReport("allocation-a", 1, 1))
	select {
	case <-firstSend:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for first condition send")
	}
	batcher.Enqueue(conditionReport("allocation-a", 1, 2))
	close(releaseFirst)

	batch := awaitConditionBatch(t, batches)
	if len(batch) != 1 || batch[0].GetConditionSet().GetRevision() != 2 {
		t.Fatalf("retried batch = %#v, want concurrent newer revision", batch)
	}
}

func conditionReport(allocationID string, attempt, revision int64) *nodev1.AllocationCapabilityConditionReport {
	now := time.Now().UTC()
	key := capabilitycontract.ExtensionKey("example.com/accelerator", "v1")
	evidence := capabilitycontract.ConfigEvidence("sha256:" + strings.Repeat("a", 64))
	observation := &capabilityv1.CapabilityObservation{
		Key:        capabilitycontract.CloneKey(key),
		State:      capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE,
		Provider:   capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_CONFIG,
		ObservedAt: timestamppb.New(now),
		Evidence:   evidence,
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
	}
	capabilitycontract.NormalizeObservation(observation)
	return &nodev1.AllocationCapabilityConditionReport{
		AllocationID: allocationID,
		Attempt:      attempt,
		ConditionSet: &capabilityv1.CapabilityConditionSet{
			Revision: revision, ObservedAt: timestamppb.New(now),
			Conditions: []*capabilityv1.CapabilityCondition{{
				Key: key, State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_HEALTHY,
				ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_AVAILABLE,
				ObservedAt: timestamppb.New(now),
				Proof:      capabilitycontract.NewObservationProof(observation),
			}},
		},
	}
}

func awaitConditionBatch(t *testing.T, batches <-chan []*nodev1.AllocationCapabilityConditionReport) []*nodev1.AllocationCapabilityConditionReport {
	t.Helper()
	select {
	case batch := <-batches:
		return batch
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for capability condition batch")
		return nil
	}
}
