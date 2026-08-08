package reservation

import (
	"fmt"
	"strconv"
	"strings"

	nodekernel "github.com/cofy-x/axern/control/controld/internal/kernel/node"
	resourcekernel "github.com/cofy-x/axern/control/controld/internal/kernel/resource"
	nodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/node/v1"
)

type reservationRejectionDiagnostics struct {
	limit                    int
	details                  []reservationRejectionDetail
	omitted                  int
	rejectedCPU              bool
	rejectedMemory           bool
	rejectedEphemeralStorage bool
	rejectedSlots            bool
}

type reservationRejectionDetail struct {
	NodeID             string
	CPU                *resourcekernel.ResourceEvaluation
	Memory             *resourcekernel.ResourceEvaluation
	EphemeralStorage   *resourcekernel.ResourceEvaluation
	CPUOvercommitRatio float64
	RuntimeSlots       *runtimeSlotEvaluation
}

type runtimeSlotEvaluation struct {
	Reserved  int64
	Active    int64
	PoolUsing int64
	Occupied  int64
	Capacity  int64
	Available int64
	Fits      bool
	Known     bool
}

func newReservationRejectionDiagnostics(limit int) reservationRejectionDiagnostics {
	return reservationRejectionDiagnostics{
		limit:   limit,
		details: make([]reservationRejectionDetail, 0, limit),
	}
}

func (d *reservationRejectionDiagnostics) Add(nodeID string, policy resourcekernel.AdmissionPolicy, evaluation resourcekernel.FitEvaluation) {
	d.AddCandidate(nodeID, policy, evaluation, runtimeSlotEvaluation{Fits: true})
}

func (d *reservationRejectionDiagnostics) AddCandidate(nodeID string, policy resourcekernel.AdmissionPolicy, evaluation resourcekernel.FitEvaluation, slots runtimeSlotEvaluation) {
	if evaluation.CPU.Requested > 0 && !evaluation.CPU.Fits {
		d.rejectedCPU = true
	}
	if evaluation.Memory.Requested > 0 && !evaluation.Memory.Fits {
		d.rejectedMemory = true
	}
	if evaluation.EphemeralStorage.Requested > 0 && !evaluation.EphemeralStorage.Fits {
		d.rejectedEphemeralStorage = true
	}
	if slots.Known && !slots.Fits {
		d.rejectedSlots = true
	}
	if len(d.details) >= d.limit {
		d.omitted++
		return
	}
	detail := buildReservationRejectionDetail(nodeID, policy, evaluation)
	if slots.Known && !slots.Fits {
		detail.RuntimeSlots = &slots
	}
	d.details = append(d.details, detail)
}

func (d reservationRejectionDiagnostics) Message() string {
	message := "no node has remaining reservation capacity"
	if len(d.details) > 0 {
		rendered := make([]string, 0, len(d.details))
		for _, detail := range d.details {
			rendered = append(rendered, detail.Message())
		}
		message += ": " + strings.Join(rendered, "; ")
	}
	if d.omitted > 0 {
		message += fmt.Sprintf("; omitted_rejections=%d", d.omitted)
	}
	return message
}

func (d reservationRejectionDiagnostics) Metadata() map[string]string {
	metadata := map[string]string{
		"diagnostic_code": string(resourcekernel.AdmissionDiagnosticNodeReservationCapacity),
		"rejection_count": strconv.Itoa(len(d.details) + d.omitted),
	}
	if d.omitted > 0 {
		metadata["omitted_rejections"] = strconv.Itoa(d.omitted)
	}
	resources := d.rejectedResources()
	if len(resources) > 0 {
		metadata["resources"] = strings.Join(resources, ",")
	}
	return metadata
}

func (d reservationRejectionDiagnostics) rejectedResources() []string {
	resources := make([]string, 0, 3)
	if d.rejectedCPU {
		resources = append(resources, "cpu")
	}
	if d.rejectedMemory {
		resources = append(resources, "memory")
	}
	if d.rejectedEphemeralStorage {
		resources = append(resources, "ephemeral_storage")
	}
	if d.rejectedSlots {
		resources = append(resources, "runtime_slots")
	}
	return resources
}

func buildReservationRejectionDetail(nodeID string, policy resourcekernel.AdmissionPolicy, evaluation resourcekernel.FitEvaluation) reservationRejectionDetail {
	detail := reservationRejectionDetail{
		NodeID:             nodeID,
		CPUOvercommitRatio: resourcekernel.NormalizeAdmissionPolicy(policy).CPUOvercommitRatio,
	}
	if evaluation.CPU.Requested > 0 && !evaluation.CPU.Fits {
		detail.CPU = &evaluation.CPU
	}
	if evaluation.Memory.Requested > 0 && !evaluation.Memory.Fits {
		detail.Memory = &evaluation.Memory
	}
	if evaluation.EphemeralStorage.Requested > 0 && !evaluation.EphemeralStorage.Fits {
		detail.EphemeralStorage = &evaluation.EphemeralStorage
	}
	return detail
}

func (d reservationRejectionDetail) Message() string {
	parts := []string{fmt.Sprintf("node_id=%s", d.NodeID)}
	if d.CPU != nil {
		parts = append(parts, fmt.Sprintf("cpu requested_milli=%d reserved_milli=%d effective_allocatable_milli=%d available_milli=%d overcommit_ratio=%.3g",
			d.CPU.Requested,
			d.CPU.Used,
			d.CPU.EffectiveAllocatable,
			d.CPU.Available,
			d.CPUOvercommitRatio,
		))
	}
	if d.Memory != nil {
		parts = append(parts, fmt.Sprintf("memory requested_bytes=%d reserved_bytes=%d effective_allocatable_bytes=%d available_bytes=%d",
			d.Memory.Requested,
			d.Memory.Used,
			d.Memory.EffectiveAllocatable,
			d.Memory.Available,
		))
	}
	if d.EphemeralStorage != nil {
		parts = append(parts, fmt.Sprintf("ephemeral_storage requested_bytes=%d reserved_bytes=%d effective_allocatable_bytes=%d available_bytes=%d",
			d.EphemeralStorage.Requested, d.EphemeralStorage.Used, d.EphemeralStorage.EffectiveAllocatable, d.EphemeralStorage.Available))
	}
	if d.RuntimeSlots != nil {
		parts = append(parts, fmt.Sprintf("runtime_slots requested=1 reserved=%d active=%d pool_using=%d occupied=%d capacity=%d available=%d",
			d.RuntimeSlots.Reserved,
			d.RuntimeSlots.Active,
			d.RuntimeSlots.PoolUsing,
			d.RuntimeSlots.Occupied,
			d.RuntimeSlots.Capacity,
			d.RuntimeSlots.Available,
		))
	}
	return strings.Join(parts, " ")
}

func evaluateRuntimeSlots(summary *nodev1.NodeSummary, reservedAllocationIDs []string) runtimeSlotEvaluation {
	capacity, known := nodekernel.RuntimeSlotCapacity(summary)
	occupancy := nodekernel.CalculateRuntimeSlotOccupancy(summary, reservedAllocationIDs)
	occupied := occupancy.Occupied
	available := capacity - occupied
	if available < 0 {
		available = 0
	}
	return runtimeSlotEvaluation{
		Reserved:  occupancy.Reserved,
		Active:    occupancy.Active,
		PoolUsing: occupancy.PoolUsing,
		Occupied:  occupied,
		Capacity:  capacity,
		Available: available,
		Fits:      known && occupied < capacity,
		Known:     known,
	}
}
