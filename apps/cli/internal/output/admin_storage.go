package output

import (
	"fmt"
	"io"
	"strings"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
)

func RenderStorageBinding(w io.Writer, binding *adminv1.StorageBinding) {
	if binding == nil {
		return
	}
	fmt.Fprintf(w, "Binding: %s\n", binding.GetBindingID())
	fmt.Fprintf(w, "Claim: %s/%s\n", binding.GetNamespace(), binding.GetClaimName())
	fmt.Fprintf(w, "Workload: %s\n", storageBindingWorkloadLabel(binding))
	fmt.Fprintf(w, "Allocation: %s\n", binding.GetAllocationID())
	fmt.Fprintf(w, "Node: %s\n", binding.GetNodeID())
	fmt.Fprintf(w, "Status: %s\n", volumeStatusLabel(binding.GetStatus()))
	if binding.GetMessage() != "" {
		fmt.Fprintf(w, "Message: %s\n", binding.GetMessage())
	}
	fmt.Fprintf(w, "Created At: %s\n", FormatProtoTimestamp(binding.GetCreatedAt()))
	fmt.Fprintf(w, "Updated At: %s\n", FormatProtoTimestamp(binding.GetUpdatedAt()))
	if binding.GetPublishedAt() != nil {
		fmt.Fprintf(w, "Published At: %s\n", FormatProtoTimestamp(binding.GetPublishedAt()))
	}
	if binding.GetReleasedAt() != nil {
		fmt.Fprintf(w, "Released At: %s\n", FormatProtoTimestamp(binding.GetReleasedAt()))
	}
}

func RenderStorageBindingTable(w io.Writer, bindings []*adminv1.StorageBinding) {
	rows := make([][]string, 0, len(bindings))
	for _, binding := range bindings {
		if binding == nil {
			continue
		}
		rows = append(rows, []string{
			binding.GetBindingID(),
			binding.GetNamespace(),
			binding.GetClaimName(),
			storageBindingWorkloadLabel(binding),
			binding.GetAllocationID(),
			binding.GetNodeID(),
			volumeStatusLabel(binding.GetStatus()),
			FormatProtoTimestamp(binding.GetUpdatedAt()),
			ShortMessage(binding.GetMessage(), 48),
		})
	}
	RenderTable(w, []string{"BINDING", "NAMESPACE", "CLAIM", "WORKLOAD", "ALLOCATION", "NODE", "STATUS", "UPDATED", "MESSAGE"}, rows)
}

func volumeStatusLabel(status storagev1.VolumeStatus) string {
	return strings.ToLower(trimEnumPrefix(status.String(), "VOLUME_STATUS_"))
}

func storageBindingWorkloadLabel(binding *adminv1.StorageBinding) string {
	workloadID := strings.TrimSpace(binding.GetWorkloadID())
	if workloadID == "" {
		return "-"
	}
	workloadType := strings.TrimSpace(binding.GetWorkloadType())
	if workloadType == "" {
		return workloadID
	}
	return workloadType + "/" + workloadID
}
