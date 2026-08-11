package output

import (
	"fmt"
	"io"
	"strings"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func RenderCapabilitySnapshot(w io.Writer, snapshot *capabilityv1.CapabilitySnapshot) {
	if snapshot == nil {
		RenderTable(w, []string{"CAPABILITY", "STATE", "PROVIDER", "IDENTITY", "AGE", "EXPIRES", "REASON", "DEPENDENCIES"}, nil)
		return
	}
	rows := make([][]string, 0, len(snapshot.GetObservations()))
	for _, observation := range snapshot.GetObservations() {
		if observation == nil {
			continue
		}
		rows = append(rows, []string{
			capabilityKeyLabel(observation.GetKey()),
			capabilityEnumLabel(observation.GetState().String(), "CAPABILITY_STATE_"),
			capabilityEnumLabel(observation.GetProvider().String(), "CAPABILITY_PROVIDER_"),
			capabilityEvidenceIdentityLabel(observation.GetEvidence()),
			capabilityAge(observation.GetObservedAt()),
			FormatProtoTimestamp(observation.GetValidUntil()),
			capabilityEnumLabel(observation.GetReasonCode().String(), "CAPABILITY_REASON_CODE_"),
			fmt.Sprintf("%d", len(observation.GetDependencies())),
		})
	}
	RenderTable(w, []string{"CAPABILITY", "STATE", "PROVIDER", "IDENTITY", "AGE", "EXPIRES", "REASON", "DEPENDENCIES"}, rows)
}

func RenderCapabilityTransitions(w io.Writer, transitions []*adminv1.AdminCapabilityTransition) {
	rows := make([][]string, 0, len(transitions))
	for _, transition := range transitions {
		if transition == nil {
			continue
		}
		rows = append(rows, []string{
			transition.GetNodeID(),
			capabilityKeyLabel(transition.GetKey()),
			capabilityProviderLabel(transition.GetKey()),
			capabilityEnumLabel(transition.GetOldState().String(), "CAPABILITY_STATE_") + "→" + capabilityEnumLabel(transition.GetNewState().String(), "CAPABILITY_STATE_"),
			capabilityEvidenceIdentityLabel(transition.GetOldEvidence()) + "→" + capabilityEvidenceIdentityLabel(transition.GetNewEvidence()),
			capabilityEnumLabel(transition.GetNewReasonCode().String(), "CAPABILITY_REASON_CODE_"),
			FormatProtoTimestamp(transition.GetObservedAt()),
			FormatProtoTimestamp(transition.GetReportedAt()),
		})
	}
	RenderTable(w, []string{"NODE", "CAPABILITY", "PROVIDER", "STATE", "EVIDENCE", "REASON", "OBSERVED", "REPORTED"}, rows)
}

func RenderCapabilityBacklog(w io.Writer, items []*adminv1.AdminCapabilityReconcileItem) {
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		if item == nil {
			continue
		}
		keys := make([]string, 0, len(item.GetPendingDependencies()))
		for _, dependency := range item.GetPendingDependencies() {
			keys = append(keys, capabilityKeyLabel(dependency.GetKey()))
		}
		rows = append(rows, []string{
			item.GetAllocationID(), item.GetNodeID(), strings.Join(keys, ","), fmt.Sprintf("%d", item.GetAttempts()),
			FormatProtoTimestamp(item.GetNextRunAt()), FormatProtoTimestamp(item.GetLeaseExpiresAt()), ShortMessage(item.GetLastError(), 80),
		})
	}
	RenderTable(w, []string{"ALLOCATION", "NODE", "PENDING", "ATTEMPTS", "NEXT", "LEASE", "LAST ERROR"}, rows)
}

func RenderAllocationCapabilityDiagnostics(w io.Writer, diagnostics *adminv1.GetAllocationCapabilityDiagnosticsResponse) {
	if diagnostics == nil {
		return
	}
	RenderTable(w, []string{"ATTEMPT", "CREATE ADMISSION", "DEPENDENCY DIGEST", "ADMITTED AT"}, [][]string{{
		fmt.Sprintf("%d", diagnostics.GetAllocationAttempt()),
		fmt.Sprintf("%t", diagnostics.GetCreateAdmissionRecorded()),
		diagnostics.GetCreateDependencySetDigest(),
		FormatProtoTimestamp(diagnostics.GetCreateAdmittedAt()),
	}})
	if admission := diagnostics.GetMemoryAdmission(); admission != nil {
		budget := admission.GetNodeMemoryBudget()
		rootLimit := "max"
		if budget.GetDelegatedRootLimitFinite() {
			rootLimit = formatBytes(budget.GetDelegatedRootLimitBytes())
		}
		RenderTable(w, []string{"MEMORY REQUEST", "MEMORY LIMIT", "BUDGET MODE", "CAPACITY IDENTITY", "PHYSICAL", "SOURCE ALLOCATABLE", "CGROUP ROOT", "SYSTEM RESERVE", "NODE EFFECTIVE", "NODE COMMITTED", "CLEANUP DEBT", "INTERNAL", "BUDGET SAMPLED", "ADMITTED"}, [][]string{{
			formatBytes(admission.GetSandboxMemoryRequestBytes()),
			formatBytes(admission.GetSandboxMemoryLimitBytes()),
			budget.GetMode().String(),
			budget.GetCapacityIdentity(),
			formatBytes(budget.GetPhysicalCapacityBytes()),
			formatBytes(budget.GetSourceAllocatableBytes()),
			rootLimit,
			formatBytes(budget.GetSystemReserveBytes()),
			formatBytes(budget.GetEffectiveAllocatableBytes()),
			formatBytes(admission.GetNodeLocalCommitmentBytes()),
			formatBytes(budget.GetCleanupDebtBytes()),
			formatBytes(budget.GetInternalCurrentBytes()),
			FormatProtoTimestamp(budget.GetSampledAt()),
			FormatProtoTimestamp(admission.GetAdmittedAt()),
		}})
	}
	rows := make([][]string, 0, len(diagnostics.GetRequiredDependencies())+len(diagnostics.GetAdmittedDependencies()))
	appendDependencies := func(kind string, dependencies []*capabilityv1.CapabilityDependency) {
		for _, dependency := range dependencies {
			if dependency == nil {
				continue
			}
			proof := dependency.GetSelectedObservation()
			rows = append(rows, []string{
				kind,
				capabilityKeyLabel(dependency.GetKey()),
				capabilityEnumLabel(dependency.GetLossPolicy().String(), "CAPABILITY_LOSS_POLICY_"),
				capabilityEnumLabel(proof.GetProvider().String(), "CAPABILITY_PROVIDER_"),
				ShortMessage(dependency.GetSelectedSnapshot().GetSnapshotID(), 18),
				proof.GetObservationID(),
				capabilityEvidenceIdentityLabel(proof.GetEvidence()),
				capabilityAge(proof.GetObservedAt()),
				FormatProtoTimestamp(proof.GetValidUntil()),
				fmt.Sprintf("%d", len(dependency.GetDependencyObservations())),
			})
		}
	}
	appendDependencies("placement", diagnostics.GetRequiredDependencies())
	appendDependencies("create", diagnostics.GetAdmittedDependencies())
	RenderTable(w, []string{"PROOF", "CAPABILITY", "LOSS POLICY", "PROVIDER", "SNAPSHOT", "OBSERVATION", "IDENTITY", "AGE", "EXPIRES", "DEPENDENCIES"}, rows)

	set := diagnostics.GetConditionSet()
	conditionRows := make([][]string, 0, len(set.GetConditions()))
	for _, condition := range set.GetConditions() {
		if condition == nil {
			continue
		}
		conditionRows = append(conditionRows, []string{
			fmt.Sprintf("%d", diagnostics.GetAllocationAttempt()),
			capabilityKeyLabel(condition.GetKey()),
			capabilityEnumLabel(condition.GetState().String(), "CAPABILITY_CONDITION_STATE_"),
			capabilityEnumLabel(condition.GetReasonCode().String(), "CAPABILITY_REASON_CODE_"),
			fmt.Sprintf("%d", set.GetRevision()),
			capabilityEnumLabel(condition.GetProof().GetProvider().String(), "CAPABILITY_PROVIDER_"),
			capabilityEvidenceIdentityLabel(condition.GetProof().GetEvidence()),
			capabilityAge(condition.GetProof().GetObservedAt()),
			FormatProtoTimestamp(condition.GetProof().GetValidUntil()),
			ShortMessage(condition.GetMessage(), 80),
		})
	}
	RenderTable(w, []string{"ATTEMPT", "CAPABILITY", "CONDITION", "REASON", "REVISION", "PROVIDER", "IDENTITY", "PROOF AGE", "PROOF EXPIRES", "MESSAGE"}, conditionRows)
	if diagnostics.GetReconcile() != nil {
		RenderCapabilityBacklog(w, []*adminv1.AdminCapabilityReconcileItem{diagnostics.GetReconcile()})
	}
	if observation := diagnostics.GetLatestMemoryObservation(); observation != nil {
		peakSource := "sampled current"
		if observation.GetPeakAvailable() {
			peakSource = "kernel memory.peak"
		}
		RenderTable(w, []string{"MEMORY CURRENT", "PEAK", "PEAK SOURCE", "ANON", "FILE", "SHMEM", "KERNEL", "DIRTY", "WRITEBACK", "OOM KILL", "CGROUP", "CLEANUP", "OBSERVED"}, [][]string{{
			formatBytes(observation.GetCurrentBytes()),
			formatBytes(observation.GetPeakBytes()),
			peakSource,
			formatBytes(observation.GetAnonBytes()),
			formatBytes(observation.GetFileBytes()),
			formatBytes(observation.GetShmemBytes()),
			formatBytes(observation.GetKernelBytes()),
			formatBytes(observation.GetDirtyBytes()),
			formatBytes(observation.GetWritebackBytes()),
			fmt.Sprintf("%d", observation.GetEventOomKill()),
			ShortMessage(observation.GetCgroupIdentity(), 18),
			strings.ToLower(strings.TrimPrefix(observation.GetCleanupState().String(), "ALLOCATION_MEMORY_CLEANUP_STATE_")),
			FormatProtoTimestamp(observation.GetObservedAt()),
		}})
	}
}

func formatBytes(value int64) string {
	if value == 0 {
		return "0 B"
	}
	const unit = int64(1024)
	if value < unit {
		return fmt.Sprintf("%d B", value)
	}
	divisor := unit
	suffix := "KiB"
	for _, next := range []string{"MiB", "GiB", "TiB"} {
		if value < divisor*unit {
			break
		}
		divisor *= unit
		suffix = next
	}
	return fmt.Sprintf("%.1f %s", float64(value)/float64(divisor), suffix)
}

func capabilityKeyLabel(key *capabilityv1.CapabilityKey) string {
	if key == nil {
		return ""
	}
	if extension := key.GetExtension(); extension != nil {
		if extension.GetValue() == "" {
			return extension.GetName()
		}
		return extension.GetName() + "=" + extension.GetValue()
	}
	return capabilityEnumLabel(key.GetPlatform().String(), "PLATFORM_CAPABILITY_")
}

func capabilityEvidenceIdentityLabel(evidence *capabilityv1.CapabilityEvidence) string {
	if evidence == nil {
		return ""
	}
	switch identity := evidence.GetIdentity().(type) {
	case *capabilityv1.CapabilityEvidence_Config:
		return "config:" + ShortMessage(identity.Config.GetConfigDigest(), 18)
	case *capabilityv1.CapabilityEvidence_Boot:
		return "boot:" + ShortMessage(identity.Boot.GetBootID(), 18)
	case *capabilityv1.CapabilityEvidence_Mount:
		return "mount:" + ShortMessage(identity.Mount.GetMountIdentity(), 18)
	case *capabilityv1.CapabilityEvidence_Runtime:
		return "runtime:" + identity.Runtime.GetRuntimeName() + ":" + ShortMessage(identity.Runtime.GetRuntimeBinaryDigest(), 12)
	case *capabilityv1.CapabilityEvidence_Derived:
		return "derived:" + ShortMessage(identity.Derived.GetCatalogDigest(), 12) + "/" + ShortMessage(identity.Derived.GetDependencyEvidenceDigest(), 12)
	default:
		return ""
	}
}

func capabilityProviderLabel(key *capabilityv1.CapabilityKey) string {
	provider, _, err := capabilitycontract.ObservationOwner(key)
	if err != nil {
		return ""
	}
	return capabilityEnumLabel(provider.String(), "CAPABILITY_PROVIDER_")
}

func capabilityAge(observedAt *timestamppb.Timestamp) string {
	if observedAt == nil {
		return ""
	}
	age := time.Since(observedAt.AsTime())
	if age < 0 {
		age = 0
	}
	return age.Round(time.Second).String()
}
