package pgrun

import (
	"context"
	"time"

	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgreservation "github.com/cofy-x/axern/control/controld/internal/postgres/reservation"
)

const (
	leaseRevisionName  = "execution_leases"
	leaseChangeChannel = "axern_execution_lease_changes"
	runChangeChannel   = "axern_run_changes"

	defaultExecutionLeaseTTL = 5 * time.Minute
)

type Store struct {
	db            *postgres.DB
	reservations  pgreservation.Admission
	leaseWatches  *changeWatchHub
	runWatches    *changeWatchHub
	reconcileWake chan struct{}
}

type Option func(*Store)

func WithAdmissionPolicy(policy resourcekernel.AdmissionPolicy) Option {
	return func(s *Store) {
		s.reservations = pgreservation.NewAdmission(policy, s.reservations.Evaluator())
	}
}

func WithPlacementEvaluator(evaluator placementkernel.Evaluator) Option {
	return func(s *Store) { s.reservations = pgreservation.NewAdmission(s.reservations.Policy(), evaluator) }
}

func NewStore(db *postgres.DB, options ...Option) *Store {
	store := &Store{
		db:            db,
		reservations:  pgreservation.NewAdmission(resourcekernel.AdmissionPolicy{}, nil),
		leaseWatches:  newChangeWatchHub(db.Pool(), leaseChangeChannel, "execution lease"),
		runWatches:    newChangeWatchHub(db.Pool(), runChangeChannel, "run"),
		reconcileWake: make(chan struct{}, 1),
	}
	for _, option := range options {
		option(store)
	}
	return store
}

func (s *Store) signalReconcileWork() {
	if s == nil {
		return
	}
	select {
	case s.reconcileWake <- struct{}{}:
	default:
	}
}

func (s *Store) WaitReconcileWork(ctx context.Context) error {
	if s == nil {
		return context.Canceled
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.reconcileWake:
		return nil
	}
}

func (s *Store) Close() {
	if s != nil && s.leaseWatches != nil {
		s.leaseWatches.close()
	}
	if s != nil && s.runWatches != nil {
		s.runWatches.close()
	}
}

var (
	_ runkernel.EnvironmentStore   = (*Store)(nil)
	_ runkernel.RunStore           = (*Store)(nil)
	_ runkernel.LeaseStore         = (*Store)(nil)
	_ runkernel.AllocationReporter = (*Store)(nil)
	_ runkernel.ReconcileStore     = (*Store)(nil)
)
