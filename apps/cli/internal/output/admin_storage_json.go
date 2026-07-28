package output

import (
	"io"

	adminv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/admin/v1"
)

type StorageBindingResponseJSON struct {
	Binding *StorageBindingJSON `json:"binding"`
}

type StorageBindingListJSON struct {
	Bindings []*StorageBindingJSON `json:"bindings"`
}

type StorageBindingJSON struct {
	BindingID    string `json:"binding_id"`
	ClaimID      string `json:"claim_id"`
	Namespace    string `json:"namespace"`
	ClaimName    string `json:"claim_name"`
	WorkloadID   string `json:"workload_id"`
	WorkloadType string `json:"workload_type"`
	AllocationID string `json:"allocation_id"`
	NodeID       string `json:"node_id"`
	Status       string `json:"status"`
	Message      string `json:"message,omitempty"`
	CreatedAt    string `json:"created_at,omitempty"`
	UpdatedAt    string `json:"updated_at,omitempty"`
	PublishedAt  string `json:"published_at,omitempty"`
	ReleasedAt   string `json:"released_at,omitempty"`
}

func PrintStorageBindingJSON(w io.Writer, binding *adminv1.StorageBinding) error {
	return PrintJSON(w, StorageBindingResponseJSON{Binding: NewStorageBindingJSON(binding)})
}

func PrintStorageBindingListJSON(w io.Writer, bindings []*adminv1.StorageBinding) error {
	out := StorageBindingListJSON{Bindings: make([]*StorageBindingJSON, 0, len(bindings))}
	for _, binding := range bindings {
		out.Bindings = append(out.Bindings, NewStorageBindingJSON(binding))
	}
	return PrintJSON(w, out)
}

func NewStorageBindingJSON(binding *adminv1.StorageBinding) *StorageBindingJSON {
	if binding == nil {
		return nil
	}
	return &StorageBindingJSON{
		BindingID:    binding.GetBindingID(),
		ClaimID:      binding.GetClaimID(),
		Namespace:    binding.GetNamespace(),
		ClaimName:    binding.GetClaimName(),
		WorkloadID:   binding.GetWorkloadID(),
		WorkloadType: binding.GetWorkloadType(),
		AllocationID: binding.GetAllocationID(),
		NodeID:       binding.GetNodeID(),
		Status:       volumeStatusLabel(binding.GetStatus()),
		Message:      binding.GetMessage(),
		CreatedAt:    FormatProtoTimestamp(binding.GetCreatedAt()),
		UpdatedAt:    FormatProtoTimestamp(binding.GetUpdatedAt()),
		PublishedAt:  FormatProtoTimestamp(binding.GetPublishedAt()),
		ReleasedAt:   FormatProtoTimestamp(binding.GetReleasedAt()),
	}
}
