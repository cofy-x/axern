package pgrun

import (
	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	placementkernel "github.com/cofy-x/axern/control/controld/internal/kernel/placement"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	runkernel "github.com/cofy-x/axern/control/controld/internal/kernel/run"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgreservation "github.com/cofy-x/axern/control/controld/internal/postgres/reservation"
)

const (
	allocationOwnerRun = allocationkernel.OwnerRun

	leaseRevisionName = "execution_leases"

	defaultExecutionLeaseTTL = 5 * time.Minute
)

type Store struct {
	db           *postgres.DB
	reservations pgreservation.Admission
	leaseWatches *leaseWatchHub
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
		db:           db,
		reservations: pgreservation.NewAdmission(resourcekernel.AdmissionPolicy{}, nil),
		leaseWatches: newLeaseWatchHub(db.Pool()),
	}
	for _, option := range options {
		option(store)
	}
	return store
}

func (s *Store) Close() {
	if s != nil && s.leaseWatches != nil {
		s.leaseWatches.close()
	}
}

var (
	_ runkernel.EnvironmentStore   = (*Store)(nil)
	_ runkernel.RunStore           = (*Store)(nil)
	_ runkernel.LeaseStore         = (*Store)(nil)
	_ runkernel.AllocationReporter = (*Store)(nil)
	_ runkernel.ReconcileStore     = (*Store)(nil)
)
