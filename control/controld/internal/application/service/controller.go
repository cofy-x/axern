package appservice

import (
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
)

const (
	defaultServiceReconcileConcurrency        = 32
	defaultAllocationCreateGlobalConcurrency  = 256
	defaultAllocationCreatePerNodeConcurrency = 12
)

type controller struct {
	store                        servicekernel.Store
	autoscaling                  servicekernel.AutoscalingSweepReader
	allocations                  servicekernel.AllocationStore
	reconcile                    servicekernel.AllocationReconcileStore
	statuses                     servicekernel.StatusStore
	events                       servicekernel.EventWriter
	environments                 servicekernel.EnvironmentReader
	selector                     servicekernel.CandidateSelector
	lifecycle                    servicekernel.AllocationLifecycle
	storage                      servicekernel.StorageCoordinator
	notifyReconcile              func(...string)
	nodeTarget                   func(string) (string, bool)
	reconcileConcurrency         int
	allocationGlobalConcurrency  int
	allocationPerNodeConcurrency int
	syncLocks                    serviceSyncLocks
}

type ControllerDeps struct {
	Store                              servicekernel.Store
	Autoscaling                        servicekernel.AutoscalingSweepReader
	Allocations                        servicekernel.AllocationStore
	Reconcile                          servicekernel.AllocationReconcileStore
	Statuses                           servicekernel.StatusStore
	Events                             servicekernel.EventWriter
	Environments                       servicekernel.EnvironmentReader
	Selector                           servicekernel.CandidateSelector
	Lifecycle                          servicekernel.AllocationLifecycle
	Storage                            servicekernel.StorageCoordinator
	NotifyReconcile                    func(...string)
	NodeTarget                         func(string) (string, bool)
	ReconcileConcurrency               int
	AllocationCreateGlobalConcurrency  int
	AllocationCreatePerNodeConcurrency int
}

func NewController(deps ControllerDeps) servicekernel.Controller {
	reconcileConcurrency := deps.ReconcileConcurrency
	if reconcileConcurrency <= 0 {
		reconcileConcurrency = defaultServiceReconcileConcurrency
	}
	allocationGlobalConcurrency := deps.AllocationCreateGlobalConcurrency
	if allocationGlobalConcurrency <= 0 {
		allocationGlobalConcurrency = defaultAllocationCreateGlobalConcurrency
	}
	allocationPerNodeConcurrency := deps.AllocationCreatePerNodeConcurrency
	if allocationPerNodeConcurrency <= 0 {
		allocationPerNodeConcurrency = defaultAllocationCreatePerNodeConcurrency
	}
	return &controller{
		store:                        deps.Store,
		autoscaling:                  deps.Autoscaling,
		allocations:                  deps.Allocations,
		reconcile:                    deps.Reconcile,
		statuses:                     deps.Statuses,
		events:                       deps.Events,
		environments:                 deps.Environments,
		selector:                     deps.Selector,
		lifecycle:                    deps.Lifecycle,
		storage:                      deps.Storage,
		notifyReconcile:              deps.NotifyReconcile,
		nodeTarget:                   deps.NodeTarget,
		reconcileConcurrency:         reconcileConcurrency,
		allocationGlobalConcurrency:  allocationGlobalConcurrency,
		allocationPerNodeConcurrency: allocationPerNodeConcurrency,
	}
}

var (
	_ servicekernel.Controller           = (*controller)(nil)
	_ servicekernel.Store                = (*controller)(nil)
	_ servicekernel.Reader               = (*controller)(nil)
	_ servicekernel.Mutator              = (*controller)(nil)
	_ servicekernel.Reconciler           = (*controller)(nil)
	_ servicekernel.AllocationReconciler = (*controller)(nil)
)
