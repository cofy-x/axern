package workspace

import (
	"errors"
	"fmt"
	"slices"

	"github.com/cofy-x/axern/internal/proto/nativebackup"
)

// RemapRetained verifies a copied, exclusive Workspace filesystem under its
// old ownership. The native maintenance helper keeps both executors stopped.
// Apply verifies the complete plan again before changing any owner record.
func (m *Manager) RemapRetained(plan nativebackup.Plan, apply bool) (nativebackup.Plan, error) {
	if err := plan.Validate(apply); err != nil {
		return nativebackup.Plan{}, err
	}
	if m.options.NodeID != plan.OldNode {
		return nativebackup.Plan{}, errors.New("native restore does not match the previous Node configuration")
	}
	// Normal provisioning can initialize an empty root. Restore requires
	// the exact root ownership that existed in the backup.
	var retainedRoot rootRecord
	if err := m.read("root.json", &retainedRoot); err != nil {
		return nativebackup.Plan{}, err
	}
	root, record, err := m.openRoot()
	if err != nil {
		return nativebackup.Plan{}, err
	}
	defer root.Close()
	entries, err := root.ReadDir(-1)
	if err != nil {
		return nativebackup.Plan{}, err
	}
	if len(entries) != len(plan.Workspaces) {
		return nativebackup.Plan{}, errors.New("restored filesystem does not contain exactly the recorded Workspaces")
	}
	plan.Workspaces = slices.Clone(plan.Workspaces)
	owners := make([]*owner, 0, len(plan.Workspaces))
	for i := range plan.Workspaces {
		row := &plan.Workspaces[i]
		spec := Spec{ID: row.ID, Namespace: row.Namespace, NodeID: plan.OldNode,
			RootIdentity: row.RootIdentity, Capacity: row.Bytes, Inodes: row.Inodes}
		if err := m.validateSpec(spec, record); err != nil {
			return nativebackup.Plan{}, err
		}
		current, err := m.readOwner(row.ID)
		if err != nil {
			return nativebackup.Plan{}, err
		}
		identity := ownerStorageIdentity(spec, current.Directory.Inode, current.Project)
		newSpec := spec
		newSpec.NodeID = plan.NewNode
		newIdentity := ownerStorageIdentity(newSpec, current.Directory.Inode, current.Project)
		if row.OldIdentity != identity || apply && row.NewIdentity != newIdentity ||
			current.State != "ready" || current.Allocation != "" || current.Retiring || len(current.Receipts) > 0 ||
			current.Generation != uint64(row.Generation) {
			return nativebackup.Plan{}, fmt.Errorf("Workspace %s is not the drained, backed-up storage binding", row.ID)
		}
		if !(current.Spec == spec && current.Identity == identity) &&
			!(apply && current.Spec == newSpec && current.Identity == newIdentity) {
			return nativebackup.Plan{}, fmt.Errorf("Workspace %s has unexpected ownership", row.ID)
		}
		if _, err := m.observe(root, current, current.Identity); err != nil {
			return nativebackup.Plan{}, fmt.Errorf("verify restored Workspace %s: %w", row.ID, err)
		}
		row.NewIdentity = newIdentity
		current.Spec, current.Identity = newSpec, newIdentity
		owners = append(owners, current)
	}
	if apply {
		for _, current := range owners {
			if err := m.writeOwner(current); err != nil {
				return nativebackup.Plan{}, err
			}
		}
	}
	return plan, nil
}
