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
		RenderTable(w, []string{"CAPABILITY", "STATE", "PROVIDER", "IDENTITY", "AGE", "EXPIRES", "REASON"}, nil)
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
		})
	}
	RenderTable(w, []string{"CAPABILITY", "STATE", "PROVIDER", "IDENTITY", "AGE", "EXPIRES", "REASON"}, rows)
}

func RenderAllocationCapabilityDiagnostics(w io.Writer, diagnostics *adminv1.GetAllocationCapabilityDiagnosticsResponse) {
	if diagnostics == nil {
		return
	}
	rows := make([][]string, 0, len(diagnostics.GetRequirements()))
	for _, requirement := range diagnostics.GetRequirements() {
		rows = append(rows, []string{capabilityKeyLabel(requirement.GetKey()), capabilityEnumLabel(requirement.GetLossPolicy().String(), "CAPABILITY_LOSS_POLICY_")})
	}
	RenderTable(w, []string{"CAPABILITY", "LOSS POLICY"}, rows)

	set := diagnostics.GetConditionSet()
	conditionRows := make([][]string, 0, len(set.GetConditions()))
	for _, condition := range set.GetConditions() {
		if condition == nil {
			continue
		}
		conditionRows = append(conditionRows, []string{
			capabilityKeyLabel(condition.GetKey()),
			capabilityEnumLabel(condition.GetState().String(), "CAPABILITY_CONDITION_STATE_"),
			capabilityEnumLabel(condition.GetReasonCode().String(), "CAPABILITY_REASON_CODE_"),
			ShortMessage(condition.GetMessage(), 80),
		})
	}
	RenderTable(w, []string{"CAPABILITY", "CONDITION", "REASON", "MESSAGE"}, conditionRows)
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
	case *capabilityv1.CapabilityEvidence_Boot:
		return "boot:" + ShortMessage(identity.Boot.GetBootID(), 18)
	case *capabilityv1.CapabilityEvidence_Mount:
		return "mount:" + ShortMessage(identity.Mount.GetMountIdentity(), 18)
	case *capabilityv1.CapabilityEvidence_Runtime:
		return "runtime:" + identity.Runtime.GetRuntimeName() + ":" + ShortMessage(identity.Runtime.GetRuntimeBinaryDigest(), 12)
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
