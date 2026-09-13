package controlplane

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
	"google.golang.org/protobuf/proto"
)

// AllocationLifecycleOutboxStore is the minimal durable node-state contract used
// by terminal allocation reporting. Keeping it independent from the service
// store interface lets the reporter own the RPC acknowledgement boundary.
type AllocationLifecycleOutboxStore interface {
	GetRecordBytes(bucket, key string) ([]byte, error)
	CompareAndSwapRecord(bucket, key string, expected []byte, expectedExists bool, next proto.Message) (bool, error)
	ForEachRecord(bucket string, visit func(key string, value []byte) error) error
}

// AllocationLifecycleOutbox retains terminal observations across axnoded process
// restarts and container-resource cleanup. Each allocation owns one current
// terminal proof. Atomic compare-and-swap prevents a concurrent acknowledgement
// or observation from overwriting/deleting that immutable proof.
type AllocationLifecycleOutbox struct {
	store AllocationLifecycleOutboxStore
}

func NewAllocationLifecycleOutbox(store AllocationLifecycleOutboxStore) *AllocationLifecycleOutbox {
	if store == nil {
		return nil
	}
	return &AllocationLifecycleOutbox{store: store}
}

func (o *AllocationLifecycleOutbox) Persist(observation *nodev1.AllocationLifecycleObservation) (bool, error) {
	if o == nil {
		return true, nil
	}
	normalized, key, err := terminalStatusOutboxRecord(observation)
	if err != nil {
		return false, err
	}
	for {
		currentData, readErr := o.store.GetRecordBytes(config.AllocationLifecycleOutboxBucket, key)
		currentExists := readErr == nil
		if readErr != nil && !errors.Is(readErr, errord.ErrNotFound) {
			return false, fmt.Errorf("read terminal allocation lifecycle %s: %w", normalized.GetAllocationID(), readErr)
		}
		if currentExists {
			current := new(nodev1.AllocationLifecycleObservation)
			if err := proto.Unmarshal(currentData, current); err != nil {
				return false, fmt.Errorf("decode current terminal allocation lifecycle %s: %w", normalized.GetAllocationID(), err)
			}
			if proto.Equal(current, normalized) {
				return true, nil
			}
			// Controld makes the first terminal state immutable. Mirror that
			// boundary locally so a later observer cannot replace the proof already
			// eligible for (or accepted by) the control plane.
			return false, nil
		}
		swapped, err := o.store.CompareAndSwapRecord(config.AllocationLifecycleOutboxBucket, key, currentData, currentExists, normalized)
		if err != nil {
			return false, fmt.Errorf("persist terminal allocation lifecycle %s: %w", normalized.GetAllocationID(), err)
		}
		if swapped {
			return true, nil
		}
	}
}

// Acknowledge removes only an exact current terminal observation after the
// control-plane RPC succeeds. A local deletion error makes the batcher retry
// the idempotent report instead of forgetting durable outbox ownership.
func (o *AllocationLifecycleOutbox) Acknowledge(observations []*nodev1.AllocationLifecycleObservation) error {
	if o == nil {
		return nil
	}
	var result error
	for _, observation := range observations {
		if observation == nil || !allocationLifecycleStopped(observation.GetState()) {
			continue
		}
		normalized, key, err := terminalStatusOutboxRecord(observation)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		for {
			currentData, readErr := o.store.GetRecordBytes(config.AllocationLifecycleOutboxBucket, key)
			if errors.Is(readErr, errord.ErrNotFound) {
				break
			}
			if readErr != nil {
				result = errors.Join(result, fmt.Errorf("read terminal allocation lifecycle %s for acknowledgement: %w", normalized.GetAllocationID(), readErr))
				break
			}
			current := new(nodev1.AllocationLifecycleObservation)
			if err := proto.Unmarshal(currentData, current); err != nil {
				result = errors.Join(result, fmt.Errorf("decode terminal allocation lifecycle %s for acknowledgement: %w", normalized.GetAllocationID(), err))
				break
			}
			if !proto.Equal(current, normalized) {
				break
			}
			swapped, err := o.store.CompareAndSwapRecord(config.AllocationLifecycleOutboxBucket, key, currentData, true, nil)
			if err != nil {
				result = errors.Join(result, fmt.Errorf("acknowledge terminal allocation lifecycle %s: %w", normalized.GetAllocationID(), err))
				break
			}
			if swapped {
				break
			}
		}
	}
	return result
}

func (o *AllocationLifecycleOutbox) Replay() ([]*nodev1.AllocationLifecycleObservation, error) {
	if o == nil {
		return nil, nil
	}
	type record struct {
		key         string
		observation *nodev1.AllocationLifecycleObservation
	}
	records := make([]record, 0)
	err := o.store.ForEachRecord(config.AllocationLifecycleOutboxBucket, func(key string, value []byte) error {
		observation := new(nodev1.AllocationLifecycleObservation)
		if err := proto.Unmarshal(value, observation); err != nil {
			return fmt.Errorf("decode terminal allocation lifecycle outbox record %s: %w", key, err)
		}
		normalized, expectedKey, err := terminalStatusOutboxRecord(observation)
		if err != nil {
			return fmt.Errorf("validate terminal allocation lifecycle outbox record %s: %w", key, err)
		}
		if key != expectedKey {
			return fmt.Errorf("terminal allocation lifecycle outbox record key mismatch: got %s want %s", key, expectedKey)
		}
		records = append(records, record{key: key, observation: normalized})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(records, func(i, j int) bool {
		left, right := records[i].observation, records[j].observation
		if left.GetAllocationID() != right.GetAllocationID() {
			return left.GetAllocationID() < right.GetAllocationID()
		}
		leftTime, rightTime := left.GetObservedAt().AsTime(), right.GetObservedAt().AsTime()
		if !leftTime.Equal(rightTime) {
			return leftTime.Before(rightTime)
		}
		return records[i].key < records[j].key
	})
	result := make([]*nodev1.AllocationLifecycleObservation, 0, len(records))
	for _, record := range records {
		result = append(result, record.observation)
	}
	return result, nil
}

func terminalStatusOutboxRecord(observation *nodev1.AllocationLifecycleObservation) (*nodev1.AllocationLifecycleObservation, string, error) {
	if observation == nil {
		return nil, "", errors.New("terminal allocation lifecycle observation is required")
	}
	normalized := proto.Clone(observation).(*nodev1.AllocationLifecycleObservation)
	normalized.AllocationID = strings.TrimSpace(normalized.GetAllocationID())
	if normalized.GetAllocationID() == "" || !allocationLifecycleStopped(normalized.GetState()) {
		return nil, "", fmt.Errorf("terminal allocation lifecycle identity is invalid")
	}
	if normalized.GetObservedAt() == nil {
		return nil, "", fmt.Errorf("terminal allocation lifecycle %s has no observation time", normalized.GetAllocationID())
	}
	if err := normalized.GetObservedAt().CheckValid(); err != nil {
		return nil, "", fmt.Errorf("terminal allocation lifecycle %s has invalid observation time: %w", normalized.GetAllocationID(), err)
	}
	return normalized, fmt.Sprintf("%x", sha256.Sum256([]byte(normalized.GetAllocationID()))), nil
}
