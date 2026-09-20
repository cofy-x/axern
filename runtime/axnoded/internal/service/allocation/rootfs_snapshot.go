package allocation

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/rootfssnapshot"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"google.golang.org/protobuf/proto"
)

type RootfsSnapshotSealingError struct{ err error }

func (e *RootfsSnapshotSealingError) Error() string { return e.err.Error() }
func (e *RootfsSnapshotSealingError) Unwrap() error { return e.err }

func WrapRootfsSnapshotSealingError(err error) error {
	if err == nil {
		return nil
	}
	return &RootfsSnapshotSealingError{err: err}
}

// IsRootfsSnapshotSealingError distinguishes a failure inside the snapshot
// barrier from a later runtime, egress, secret, or Allocation-state cleanup
// failure. The distinction is transient and never becomes another node state.
func IsRootfsSnapshotSealingError(err error) bool {
	var target *RootfsSnapshotSealingError
	return errors.As(err, &target)
}

func immutableImageDigest(ref string) (string, error) {
	_, digest, ok := strings.Cut(strings.TrimSpace(ref), "@")
	algorithm, encoded, valid := strings.Cut(strings.TrimSpace(digest), ":")
	if !ok || !valid || algorithm != "sha256" || len(encoded) != 64 {
		return "", fmt.Errorf("immutable sha256 image reference is required: %w", errord.ErrInvalidArgument)
	}
	if _, err := hex.DecodeString(encoded); err != nil {
		return "", fmt.Errorf("invalid image digest: %w", errord.ErrInvalidArgument)
	}
	return algorithm + ":" + strings.ToLower(encoded), nil
}

func rootfsSnapshotContractDigest(allocationID, baseImageDigest string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(allocationID) + "\x00" + strings.TrimSpace(baseImageDigest)))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (h *Controller) sealRootfsSnapshot(ctx context.Context, allocationID string, request *apipb.RootfsSnapshotSealingRequest) (*apipb.RootfsSnapshotSealingResult, error) {
	allocationID = strings.TrimSpace(allocationID)
	baseImageRef := strings.TrimSpace(request.GetBaseImageRef())
	if allocationID == "" || baseImageRef == "" {
		return nil, fmt.Errorf("rootfs snapshot allocation and base image are required: %w", errord.ErrInvalidArgument)
	}
	baseImageDigest, err := immutableImageDigest(baseImageRef)
	if err != nil {
		return nil, err
	}
	contractDigest := rootfsSnapshotContractDigest(allocationID, baseImageDigest)
	var receipt apipb.RootfsSnapshotReceipt
	if err := h.store.GetRecord(config.RootfsSnapshotReceiptBucket, allocationID, &receipt); err == nil {
		if receipt.GetAllocationID() != allocationID || receipt.GetContractDigest() != contractDigest || receipt.GetResult() == nil {
			return nil, fmt.Errorf("rootfs snapshot receipt conflicts with cleanup request: %w", errord.ErrFailedPrecondition)
		}
		if nodeID := strings.TrimSpace(h.config.ControlPlaneNodeID); nodeID == "" || receipt.GetNodeID() != nodeID {
			return nil, fmt.Errorf("rootfs snapshot receipt node identity conflicts with this node: %w", errord.ErrFailedPrecondition)
		}
		return proto.Clone(receipt.GetResult()).(*apipb.RootfsSnapshotSealingResult), nil
	} else if !errors.Is(err, errord.ErrNotFound) {
		return nil, fmt.Errorf("load rootfs snapshot receipt: %w", err)
	}

	h.stateMu.RLock()
	state := h.allocationStates[allocationID]
	var record *apipb.AllocationState
	if state != nil {
		record = cloneAllocationRecord(state.record)
	}
	h.stateMu.RUnlock()
	if record == nil || record.GetRootfsSnapshot() == nil {
		return nil, fmt.Errorf("rootfs snapshot recovery contract is missing: %w", errord.ErrFailedPrecondition)
	}
	localBaseDigest, err := immutableImageDigest(record.GetEnvironment().GetRootfs().GetImageUrl())
	if err != nil || localBaseDigest != baseImageDigest {
		return nil, fmt.Errorf("rootfs snapshot base image conflicts with durable allocation contract: %w", errord.ErrFailedPrecondition)
	}
	if h.rootfsSnapshots == nil {
		return nil, fmt.Errorf("rootfs snapshot publication is unavailable: %w", errord.ErrUnavailable)
	}
	snapshotter, ok := h.runscHandler.(contract.RootfsSnapshotter)
	if !ok {
		return nil, fmt.Errorf("runtime cannot export rootfs snapshots: %w", errord.ErrNotImplemented)
	}
	target, err := h.containers().Get(allocationID)
	if err != nil || target == nil || target.Status == nil {
		return nil, fmt.Errorf("rootfs snapshot runtime is unavailable: %w", errord.ErrFailedPrecondition)
	}
	if target.Status.Get().State() != apipb.ContainerState_CONTAINER_EXITED {
		return nil, fmt.Errorf("rootfs snapshot requires a terminal workload: %w", errord.ErrFailedPrecondition)
	}

	maxBytes := record.GetResources().GetLimits().GetEphemeralStorageBytes()
	if maxBytes <= 0 {
		return nil, fmt.Errorf("rootfs snapshot has no durable ephemeral-storage limit: %w", errord.ErrFailedPrecondition)
	}
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	upperLayer, upperLayerWriter := io.Pipe()
	type publication struct {
		result *apipb.RootfsSnapshotSealingResult
		err    error
	}
	published := make(chan publication, 1)
	go func() {
		result, publishErr := h.rootfsSnapshots.Publish(streamCtx, rootfssnapshot.Request{
			AllocationID: allocationID, BaseImageRef: baseImageRef,
			BaseRegistryCredential: request.GetBaseRegistryCredentialJson(), UpperLayer: upperLayer, MaxBytes: maxBytes,
		})
		_ = upperLayer.CloseWithError(publishErr)
		if publishErr != nil {
			cancel()
		}
		published <- publication{result: result, err: publishErr}
	}()
	exportErr := snapshotter.SnapshotRootfsUpper(streamCtx, allocationID, upperLayerWriter)
	_ = upperLayerWriter.CloseWithError(exportErr)
	publicationResult := <-published
	if publicationResult.err != nil {
		return nil, publicationResult.err
	}
	if exportErr != nil {
		return nil, exportErr
	}
	result := publicationResult.result
	if result == nil || strings.TrimSpace(result.GetImageRef()) == "" || result.GetImageDescriptor() == nil {
		return nil, fmt.Errorf("rootfs snapshot publisher returned an incomplete result: %w", errord.ErrUnavailable)
	}
	receipt = apipb.RootfsSnapshotReceipt{
		AllocationID: allocationID, NodeID: record.GetNodeID(), ContractDigest: contractDigest,
		Result: proto.Clone(result).(*apipb.RootfsSnapshotSealingResult),
	}
	if err := h.store.PutRecord(config.RootfsSnapshotReceiptBucket, allocationID, &receipt); err != nil {
		return nil, fmt.Errorf("persist rootfs snapshot receipt: %w", err)
	}
	return result, nil
}

// AcknowledgeRootfsSnapshotRelease retires only the crash-recovery receipt.
// Runtime and Allocation cleanup remains owned by DeleteAllocation.
func (h *Controller) AcknowledgeRootfsSnapshotRelease(allocationID, nodeID string) error {
	allocationID, nodeID = strings.TrimSpace(allocationID), strings.TrimSpace(nodeID)
	if allocationID == "" || nodeID == "" {
		return errord.ErrInvalidArgument
	}
	var receipt apipb.RootfsSnapshotReceipt
	if err := h.store.GetRecord(config.RootfsSnapshotReceiptBucket, allocationID, &receipt); err != nil {
		if errors.Is(err, errord.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("load rootfs snapshot receipt: %w", err)
	}
	if receipt.GetAllocationID() != allocationID || receipt.GetNodeID() != nodeID {
		return fmt.Errorf("rootfs snapshot receipt identity mismatch: %w", errord.ErrFailedPrecondition)
	}
	return h.store.DeleteRecord(config.RootfsSnapshotReceiptBucket, allocationID)
}

func (h *Controller) RootfsSnapshotPending(allocationID string) (bool, error) {
	allocationID = strings.TrimSpace(allocationID)
	h.stateMu.RLock()
	state := h.allocationStates[allocationID]
	pending := state != nil && state.record.GetRootfsSnapshot() != nil
	h.stateMu.RUnlock()
	if state == nil {
		var record apipb.AllocationState
		if err := h.store.GetRecord(config.AllocationStateBucket, allocationID, &record); err == nil {
			pending = record.GetRootfsSnapshot() != nil
		} else if !errors.Is(err, errord.ErrNotFound) {
			return false, fmt.Errorf("load rootfs snapshot allocation state: %w", err)
		}
	}
	if !pending {
		return false, nil
	}
	var receipt apipb.RootfsSnapshotReceipt
	if err := h.store.GetRecord(config.RootfsSnapshotReceiptBucket, allocationID, &receipt); err == nil {
		return false, nil
	} else if errors.Is(err, errord.ErrNotFound) {
		return true, nil
	} else {
		return false, fmt.Errorf("load rootfs snapshot receipt: %w", err)
	}
}
