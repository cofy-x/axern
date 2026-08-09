package container

import (
	"errors"
	"fmt"

	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/sirupsen/logrus"
)

// ReconcileResourceClaims removes persisted pool ownership that has no
// corresponding container claim. The caller must first validate one complete
// runtime inventory generation. Otherwise missing container metadata could
// make a live runtime's resource ownership look unclaimed.
func (m *Manager) ReconcileResourceClaims() error {
	claimed := make(map[resourcemanager.ResourceName]map[string]struct{})
	for id, c := range m.containers.Items() {
		if c == nil || c.Spec == nil || c.Spec.Version == "" {
			return fmt.Errorf("reconcile resource claims: container %s has no recoverable OCI spec", id)
		}
		for name, resourceID := range collectResourceFromSpec(id, c.Spec).Resources {
			if claimed[name] == nil {
				claimed[name] = make(map[string]struct{})
			}
			claimed[name][resourceID] = struct{}{}
		}
	}

	var reconcileErr error
	for item := range m.resourceManagers.IterBuffered() {
		manager := item.Val
		using, _ := manager.Status()
		owned := claimed[manager.ResourceName()]
		for _, resourceID := range using {
			if _, ok := owned[resourceID]; ok {
				continue
			}
			if err := manager.Recycle(resourceID); err != nil {
				reconcileErr = errors.Join(reconcileErr, fmt.Errorf("recycle unclaimed %s resource %s: %w", manager.ResourceName(), resourceID, err))
				continue
			}
			logrus.Warnf("recycled unclaimed %s resource %s during startup recovery", manager.ResourceName(), resourceID)
		}
	}
	return reconcileErr
}
