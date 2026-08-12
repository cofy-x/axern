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

// AllocationStatusOutboxStore is the minimal durable node-state contract used
// by terminal allocation reporting. Keeping it independent from the service
// store interface lets the reporter own the RPC acknowledgement boundary.
type AllocationStatusOutboxStore interface {
	GetRecordBytes(bucket, key string) ([]byte, error)
	CompareAndSwapRecord(bucket, key string, expected []byte, expectedExists bool, next proto.Message) (bool, error)
	ForEachRecord(bucket string, visit func(key string, value []byte) error) error
}

// AllocationStatusOutbox retains terminal observations across axnoded process
// restarts and container-resource cleanup. Each allocation owns one current
// terminal proof. Atomic compare-and-swap prevents an older acknowledgement or
// observation from overwriting/deleting a newer attempt.
type AllocationStatusOutbox struct {
	store AllocationStatusOutboxStore
}

func NewAllocationStatusOutbox(store AllocationStatusOutboxStore) *AllocationStatusOutbox {
	if store == nil {
		return nil
	}
	return &AllocationStatusOutbox{store: store}
}

func (o *AllocationStatusOutbox) Persist(observation *nodev1.AllocationStatusObservation) (bool, error) {
	if o == nil {
		return true, nil
	}
	normalized, key, err := terminalStatusOutboxRecord(observation)
	if err != nil {
		return false, err
	}
	for {
		currentData, readErr := o.store.GetRecordBytes(config.AllocationStatusOutboxBucket, key)
		currentExists := readErr == nil
		if readErr != nil && !errors.Is(readErr, errord.ErrNotFound) {
			return false, fmt.Errorf("read terminal allocation status %s: %w", normalized.GetAllocationID(), readErr)
		}
		if currentExists {
			current := new(nodev1.AllocationStatusObservation)
			if err := proto.Unmarshal(currentData, current); err != nil {
				return false, fmt.Errorf("decode current terminal allocation status %s: %w", normalized.GetAllocationID(), err)
			}
			if current.GetAttempt() > normalized.GetAttempt() {
				return false, nil
			}
			if current.GetAttempt() == normalized.GetAttempt() {
				if proto.Equal(current, normalized) {
					return true, nil
				}
				// Controld makes the first terminal state for an attempt immutable.
				// Mirror that boundary locally: a later liveness/runtime observer may
				// produce a different terminal projection, but it cannot replace the
				// proof already eligible for (or accepted by) the control plane.
				return false, nil
			}
		}
		swapped, err := o.store.CompareAndSwapRecord(config.AllocationStatusOutboxBucket, key, currentData, currentExists, normalized)
		if err != nil {
			return false, fmt.Errorf("persist terminal allocation status %s: %w", normalized.GetAllocationID(), err)
		}
		if swapped {
			return true, nil
		}
	}
}

// Acknowledge removes only an exact current terminal observation after the
// control-plane RPC succeeds. A local deletion error makes the batcher retry
// the idempotent report instead of forgetting durable outbox ownership.
func (o *AllocationStatusOutbox) Acknowledge(observations []*nodev1.AllocationStatusObservation) error {
	if o == nil {
		return nil
	}
	var result error
	for _, observation := range observations {
		if observation == nil || !allocationStatusEnded(observation.GetStatus()) {
			continue
		}
		normalized, key, err := terminalStatusOutboxRecord(observation)
		if err != nil {
			result = errors.Join(result, err)
			continue
		}
		for {
			currentData, readErr := o.store.GetRecordBytes(config.AllocationStatusOutboxBucket, key)
			if errors.Is(readErr, errord.ErrNotFound) {
				break
			}
			if readErr != nil {
				result = errors.Join(result, fmt.Errorf("read terminal allocation status %s for acknowledgement: %w", normalized.GetAllocationID(), readErr))
				break
			}
			current := new(nodev1.AllocationStatusObservation)
			if err := proto.Unmarshal(currentData, current); err != nil {
				result = errors.Join(result, fmt.Errorf("decode terminal allocation status %s for acknowledgement: %w", normalized.GetAllocationID(), err))
				break
			}
			if !proto.Equal(current, normalized) {
				break
			}
			swapped, err := o.store.CompareAndSwapRecord(config.AllocationStatusOutboxBucket, key, currentData, true, nil)
			if err != nil {
				result = errors.Join(result, fmt.Errorf("acknowledge terminal allocation status %s: %w", normalized.GetAllocationID(), err))
				break
			}
			if swapped {
				break
			}
		}
	}
	return result
}

func (o *AllocationStatusOutbox) Replay() ([]*nodev1.AllocationStatusObservation, error) {
	if o == nil {
		return nil, nil
	}
	type record struct {
		key         string
		observation *nodev1.AllocationStatusObservation
	}
	records := make([]record, 0)
	err := o.store.ForEachRecord(config.AllocationStatusOutboxBucket, func(key string, value []byte) error {
		observation := new(nodev1.AllocationStatusObservation)
		if err := proto.Unmarshal(value, observation); err != nil {
			return fmt.Errorf("decode terminal allocation status outbox record %s: %w", key, err)
		}
		normalized, expectedKey, err := terminalStatusOutboxRecord(observation)
		if err != nil {
			return fmt.Errorf("validate terminal allocation status outbox record %s: %w", key, err)
		}
		if key != expectedKey {
			return fmt.Errorf("terminal allocation status outbox record key mismatch: got %s want %s", key, expectedKey)
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
		if left.GetAttempt() != right.GetAttempt() {
			return left.GetAttempt() < right.GetAttempt()
		}
		leftTime, rightTime := left.GetObservedAt().AsTime(), right.GetObservedAt().AsTime()
		if !leftTime.Equal(rightTime) {
			return leftTime.Before(rightTime)
		}
		return records[i].key < records[j].key
	})
	result := make([]*nodev1.AllocationStatusObservation, 0, len(records))
	for _, record := range records {
		result = append(result, record.observation)
	}
	return result, nil
}

func terminalStatusOutboxRecord(observation *nodev1.AllocationStatusObservation) (*nodev1.AllocationStatusObservation, string, error) {
	if observation == nil {
		return nil, "", errors.New("terminal allocation status observation is required")
	}
	normalized := proto.Clone(observation).(*nodev1.AllocationStatusObservation)
	normalized.AllocationID = strings.TrimSpace(normalized.GetAllocationID())
	if normalized.GetAllocationID() == "" || normalized.GetAttempt() <= 0 || !allocationStatusEnded(normalized.GetStatus()) {
		return nil, "", fmt.Errorf("terminal allocation status identity is invalid")
	}
	if normalized.GetObservedAt() == nil {
		return nil, "", fmt.Errorf("terminal allocation status %s has no observation time", normalized.GetAllocationID())
	}
	if err := normalized.GetObservedAt().CheckValid(); err != nil {
		return nil, "", fmt.Errorf("terminal allocation status %s has invalid observation time: %w", normalized.GetAllocationID(), err)
	}
	return normalized, fmt.Sprintf("%x", sha256.Sum256([]byte(normalized.GetAllocationID()))), nil
}
