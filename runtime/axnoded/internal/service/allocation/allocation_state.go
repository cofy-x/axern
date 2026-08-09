package allocation

import (
	"context"
	"errors"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	langruntime "github.com/cofy-x/axern/runtime/axnoded/internal/langruntime"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"github.com/sirupsen/logrus"
	"google.golang.org/protobuf/proto"
)

type allocationState struct {
	record          *apipb.AllocationState
	runtime         *langruntime.LanguageRuntime
	imageMountRoots []*langruntime.RootFS
	workspace       workspaceImageRecord
}

func newAllocationState(allocationID string) *allocationState {
	return &allocationState{record: &apipb.AllocationState{AllocationID: allocationID}}
}

func (h *Controller) stateLocked(allocationID string) *allocationState {
	state := h.allocationStates[allocationID]
	if state == nil {
		state = newAllocationState(allocationID)
		h.allocationStates[allocationID] = state
	}
	return state
}

func cloneAllocationRecord(record *apipb.AllocationState) *apipb.AllocationState {
	if record == nil {
		return nil
	}
	return proto.Clone(record).(*apipb.AllocationState)
}

func allocationRecordEmpty(record *apipb.AllocationState) bool {
	return record == nil || (record.GetRuntimeTemplate() == nil && len(record.GetImageMountUrls()) == 0 && record.GetWorkspaceImageUrl() == "" && len(record.GetCapabilityDependencies()) == 0)
}

// PrepareCapabilityDependencies is the first durable side effect of create.
// It makes admission dependencies recoverable before volumes, rootfs, mounts,
// cgroups, or runtime processes are touched.
func (h *Controller) PrepareCapabilityDependencies(allocationID string, dependencies []*capabilityv1.CapabilityDependency) error {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return errors.New("allocation id is required")
	}
	desired := &apipb.AllocationState{AllocationID: allocationID}
	h.stateMu.RLock()
	if current := h.allocationStates[allocationID]; current != nil {
		desired = cloneAllocationRecord(current.record)
	}
	h.stateMu.RUnlock()
	desired.CapabilityDependencies = cloneCapabilityDependencies(dependencies)
	if err := h.persistAllocationRecord(desired); err != nil {
		return fmt.Errorf("persist allocation capability dependencies: %w", err)
	}
	h.stateMu.Lock()
	state := h.stateLocked(allocationID)
	state.record = desired
	h.stateMu.Unlock()
	return nil
}

func (h *Controller) CapabilityDependencyManifests() map[string][]*capabilityv1.CapabilityDependency {
	result := make(map[string][]*capabilityv1.CapabilityDependency)
	h.stateMu.RLock()
	defer h.stateMu.RUnlock()
	for allocationID, state := range h.allocationStates {
		if state != nil && len(state.record.GetCapabilityDependencies()) > 0 {
			result[allocationID] = cloneCapabilityDependencies(state.record.GetCapabilityDependencies())
		}
	}
	return result
}

func cloneCapabilityDependencies(in []*capabilityv1.CapabilityDependency) []*capabilityv1.CapabilityDependency {
	out := make([]*capabilityv1.CapabilityDependency, 0, len(in))
	for _, dependency := range in {
		if dependency != nil {
			out = append(out, proto.Clone(dependency).(*capabilityv1.CapabilityDependency))
		}
	}
	return out
}

func (h *Controller) persistAllocationRecord(record *apipb.AllocationState) error {
	if record == nil || strings.TrimSpace(record.GetAllocationID()) == "" {
		return errors.New("allocation state requires an allocation id")
	}
	if allocationRecordEmpty(record) {
		return h.store.DeleteRecord(config.AllocationStateBucket, record.GetAllocationID())
	}
	return h.store.PutRecord(config.AllocationStateBucket, record.GetAllocationID(), record)
}

func (h *Controller) rememberContainerRuntime(allocationID string, runtime *langruntime.LanguageRuntime) error {
	allocationID = strings.TrimSpace(allocationID)
	if allocationID == "" {
		return errors.New("allocation id is required")
	}
	if runtime == nil || runtime.RuntimeTemplate() == nil {
		return errors.New("allocation runtime template is required")
	}
	h.stateMu.RLock()
	current := h.allocationStates[allocationID]
	if current != nil && current.runtime != nil && current.runtime != runtime {
		h.stateMu.RUnlock()
		return errors.New("allocation runtime is already registered")
	}
	var desired *apipb.AllocationState
	if current == nil {
		desired = &apipb.AllocationState{AllocationID: allocationID}
	} else {
		desired = cloneAllocationRecord(current.record)
	}
	h.stateMu.RUnlock()
	desired.RuntimeTemplate = proto.Clone(runtime.RuntimeTemplate()).(*apipb.RuntimeTemplate)
	if err := h.persistAllocationRecord(desired); err != nil {
		return fmt.Errorf("persist allocation runtime: %w", err)
	}
	h.stateMu.Lock()
	state := h.stateLocked(allocationID)
	state.record = desired
	state.runtime = runtime
	h.stateMu.Unlock()
	return nil
}

func (h *Controller) rememberImageMountRoots(allocationID string, roots []*langruntime.RootFS, mounts []*apipb.ImageMount) error {
	allocationID = strings.TrimSpace(allocationID)
	if h == nil || allocationID == "" || len(roots) == 0 {
		return nil
	}
	h.stateMu.Lock()
	state := h.stateLocked(allocationID)
	if len(state.imageMountRoots) > 0 {
		h.stateMu.Unlock()
		return errors.New("allocation image mounts are already registered")
	}
	state.record.ImageMountUrls = state.record.ImageMountUrls[:0]
	for _, mount := range mounts {
		if mount != nil && strings.TrimSpace(mount.GetImage()) != "" {
			state.record.ImageMountUrls = append(state.record.ImageMountUrls, strings.TrimSpace(mount.GetImage()))
		}
	}
	state.imageMountRoots = append(state.imageMountRoots, roots...)
	h.stateMu.Unlock()
	return nil
}

func (h *Controller) forgetImageMountRoots(allocationID string) {
	if h == nil || allocationID == "" {
		return
	}
	h.stateMu.RLock()
	state := h.allocationStates[allocationID]
	if state == nil {
		h.stateMu.RUnlock()
		return
	}
	desired := cloneAllocationRecord(state.record)
	roots := append([]*langruntime.RootFS(nil), state.imageMountRoots...)
	committed := state.runtime != nil
	h.stateMu.RUnlock()
	desired.ImageMountUrls = nil
	if committed {
		if err := h.persistAllocationRecord(desired); err != nil {
			logrus.WithError(err).WithField("allocation_id", allocationID).Warn("persist released image mount ownership")
			return
		}
	}
	h.stateMu.Lock()
	state = h.allocationStates[allocationID]
	if state != nil {
		state.record = desired
		state.imageMountRoots = nil
		if allocationRecordEmpty(state.record) && state.workspace.cleanup == nil && state.runtime == nil {
			delete(h.allocationStates, allocationID)
		}
	}
	h.stateMu.Unlock()
	releaseImageMountRoots(roots)
}

func (h *Controller) rememberWorkspaceImage(allocationID string, workspace workspaceImageRecord) {
	h.stateMu.Lock()
	state := h.stateLocked(allocationID)
	previous := state.workspace
	state.workspace = workspace
	h.stateMu.Unlock()
	if previous.cleanup != nil {
		previous.cleanup()
	}
}

func (h *Controller) rememberWorkspaceImageSpec(allocationID, imageURL, sourcePath, target string) error {
	h.stateMu.Lock()
	state := h.stateLocked(allocationID)
	state.record.WorkspaceImageUrl = strings.TrimSpace(imageURL)
	state.record.WorkspaceSourcePath = strings.TrimSpace(sourcePath)
	state.record.WorkspaceTarget = strings.TrimSpace(target)
	h.stateMu.Unlock()
	return nil
}

func (h *Controller) forgetWorkspaceImage(allocationID string) {
	h.stateMu.RLock()
	state := h.allocationStates[allocationID]
	if state == nil {
		h.stateMu.RUnlock()
		return
	}
	desired := cloneAllocationRecord(state.record)
	workspace := state.workspace
	committed := state.runtime != nil
	h.stateMu.RUnlock()
	desired.WorkspaceImageUrl = ""
	desired.WorkspaceSourcePath = ""
	desired.WorkspaceTarget = ""
	if committed {
		if err := h.persistAllocationRecord(desired); err != nil {
			logrus.WithError(err).WithField("allocation_id", allocationID).Warn("persist released workspace image ownership")
			return
		}
	}
	h.stateMu.Lock()
	state = h.allocationStates[allocationID]
	if state != nil {
		state.record = desired
		state.workspace = workspaceImageRecord{}
		if allocationRecordEmpty(state.record) && len(state.imageMountRoots) == 0 && state.runtime == nil {
			delete(h.allocationStates, allocationID)
		}
	}
	h.stateMu.Unlock()
	if workspace.cleanup != nil {
		workspace.cleanup()
	}
}

func (h *Controller) releaseAllocationState(allocationID string) error {
	if strings.TrimSpace(allocationID) == "" {
		return nil
	}
	if err := h.store.DeleteRecord(config.AllocationStateBucket, allocationID); err != nil {
		return fmt.Errorf("delete allocation state: %w", err)
	}
	h.stateMu.Lock()
	state := h.allocationStates[allocationID]
	delete(h.allocationStates, allocationID)
	h.stateMu.Unlock()
	if state == nil {
		return nil
	}
	if state.runtime != nil {
		state.runtime.DecRef()
		if state.runtime.Retained() {
			h.scheduleExecutionEnvelopePrepare(state.runtime)
		}
	}
	releaseImageMountRoots(state.imageMountRoots)
	if state.workspace.cleanup != nil {
		state.workspace.cleanup()
	}
	return nil
}

func (h *Controller) loadAllocationStates() error {
	var recoveryErr error
	err := h.store.ForEachRecord(config.AllocationStateBucket, func(key string, value []byte) error {
		if _, err := h.containers().Get(key); err != nil {
			if err := h.store.DeleteRecord(config.AllocationStateBucket, key); err != nil {
				recoveryErr = errors.Join(recoveryErr, fmt.Errorf("delete orphan allocation state %s: %w", key, err))
			}
			return nil
		}
		var record apipb.AllocationState
		if err := proto.Unmarshal(value, &record); err != nil {
			recoveryErr = errors.Join(recoveryErr, fmt.Errorf("decode allocation state %s: %w", key, err))
			return nil
		}
		if record.GetAllocationID() == "" || record.GetAllocationID() != key {
			recoveryErr = errors.Join(recoveryErr, fmt.Errorf("allocation state key %s does not match record id %s", key, record.GetAllocationID()))
			return nil
		}
		state, err := h.restoreAllocationState(&record)
		h.stateMu.Lock()
		h.allocationStates[key] = state
		h.stateMu.Unlock()
		if err != nil {
			recoveryErr = errors.Join(recoveryErr, fmt.Errorf("restore allocation %s state: %w", key, err))
		}
		return nil
	})
	return errors.Join(err, recoveryErr)
}

func (h *Controller) restoreAllocationState(record *apipb.AllocationState) (*allocationState, error) {
	state := &allocationState{record: cloneAllocationRecord(record)}
	var recoveryErr error
	if record.GetRuntimeTemplate() == nil {
		recoveryErr = errors.Join(recoveryErr, errors.New("active allocation has no runtime template"))
	} else {
		rootfsConfig, err := langruntime.RootfsConfigFromRuntimeTemplate(record.GetRuntimeTemplate())
		if err != nil {
			recoveryErr = errors.Join(recoveryErr, err)
		} else {
			result, err := h.lrtManager.AddLangRuntime(context.Background(), record.GetRuntimeTemplate(), rootfsConfig, true)
			if err != nil {
				recoveryErr = errors.Join(recoveryErr, err)
			} else {
				state.runtime = result.Runtime
				state.runtime.IncRef()
			}
		}
	}
	if err := h.restoreAllocationImages(record, state); err != nil {
		recoveryErr = errors.Join(recoveryErr, err)
	}
	return state, recoveryErr
}

func (h *Controller) restoreAllocationImages(record *apipb.AllocationState, state *allocationState) error {
	for _, imageURL := range record.GetImageMountUrls() {
		rootfs, err := h.acquireRecoveredImageRoot(imageURL)
		if err != nil {
			return err
		}
		state.imageMountRoots = append(state.imageMountRoots, rootfs)
	}
	if record.GetWorkspaceImageUrl() == "" {
		return nil
	}
	if err := validateWorkspaceImage(&apipb.WorkspaceImageSource{
		Variants:   []*apipb.WorkspaceImageVariant{{Format: "oci", Image: record.GetWorkspaceImageUrl()}},
		SourcePath: record.GetWorkspaceSourcePath(),
		Target:     record.GetWorkspaceTarget(),
	}); err != nil {
		return err
	}
	rootfs, err := h.acquireRecoveredImageRoot(record.GetWorkspaceImageUrl())
	if err != nil {
		return err
	}
	workspaceRoot := filepath.Join(h.config.RuntimeConfig.FilestoreDir, workspaceViewsDir, record.GetAllocationID())
	lower, err := workspaceLowerPath(rootfs.Path(), record.GetWorkspaceSourcePath())
	if err != nil {
		state.imageMountRoots = append(state.imageMountRoots, rootfs)
		return err
	}
	merged, err := restoreWorkspaceCOW(workspaceRoot, lower)
	if err != nil {
		state.imageMountRoots = append(state.imageMountRoots, rootfs)
		return err
	}
	state.workspace = workspaceImageRecord{
		payloadRoot: rootfs.Path(),
		taskRoot:    strings.TrimSuffix(path.Clean(record.GetWorkspaceSourcePath()), "/workspace"),
		merged:      merged,
		target:      record.GetWorkspaceTarget(),
		cleanup: func() {
			if err := cleanupWorkspaceCOW(workspaceRoot); err != nil {
				logrus.WithError(err).Warn("cleanup recovered workspace view")
			}
			rootfs.ReleaseActiveRef()
		},
	}
	return nil
}

func (h *Controller) acquireRecoveredImageRoot(imageURL string) (*langruntime.RootFS, error) {
	config, err := h.lrtManager.ResolveRootfsConfig(langruntime.RootfsConfig{SrcType: apipb.RootfsSrcType_IMAGE, ImageUrl: imageURL})
	if err != nil {
		return nil, err
	}
	rootfs, err := h.lrtManager.GetRootfs(config)
	if err != nil {
		return nil, err
	}
	if err := rootfs.IncActiveRef(); err != nil {
		return nil, err
	}
	return rootfs, nil
}
