package pgservice

import (
	allocationkernel "github.com/cofy-x/axern/control/controld/internal/kernel/allocation"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	servicekernel "github.com/cofy-x/axern/control/controld/internal/kernel/service"
	"github.com/cofy-x/axern/control/controld/internal/postgres"
	pgreservation "github.com/cofy-x/axern/control/controld/internal/postgres/reservation"
)

const leaseRevisionName = "execution_leases"
const allocationOwnerService = allocationkernel.OwnerService

type PGStore struct {
	db           *postgres.DB
	reservations pgreservation.Admission
	watches      *watchHub
}

type Option func(*PGStore)

func WithAdmissionPolicy(policy resourcekernel.AdmissionPolicy) Option {
	return func(s *PGStore) {
		s.reservations = pgreservation.NewAdmission(policy)
	}
}

func NewPGStore(db *postgres.DB, options ...Option) *PGStore {
	store := &PGStore{
		db:           db,
		reservations: pgreservation.NewAdmission(resourcekernel.AdmissionPolicy{}),
		watches:      newWatchHub(db.Pool()),
	}
	for _, option := range options {
		option(store)
	}
	return store
}

var (
	_ servicekernel.Store                  = (*PGStore)(nil)
	_ servicekernel.Reader                 = (*PGStore)(nil)
	_ servicekernel.Watcher                = (*PGStore)(nil)
	_ servicekernel.Mutator                = (*PGStore)(nil)
	_ servicekernel.AutoscalingSweepReader = (*PGStore)(nil)
	_ servicekernel.ReplicaReader          = (*PGStore)(nil)
	_ servicekernel.EventReader            = (*PGStore)(nil)
	_ servicekernel.EventWriter            = (*PGStore)(nil)
	_ servicekernel.AllocationStore        = (*PGStore)(nil)
	_ servicekernel.StatusStore            = (*PGStore)(nil)
	_ servicekernel.AllocationReporter     = (*PGStore)(nil)
)
