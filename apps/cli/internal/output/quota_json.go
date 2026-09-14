package output

import (
	"io"

	quotav1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/quota/v1"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

type NamespaceQuotaResponseJSON struct {
	Quota *NamespaceQuotaJSON `json:"quota"`
}

type NamespaceQuotaListJSON struct {
	Quotas []*NamespaceQuotaJSON `json:"quotas"`
}

type NamespaceQuotaEventsJSON struct {
	Events []*NamespaceQuotaEventJSON `json:"events"`
}

type NamespaceQuotaEventJSON struct {
	ID                             string `json:"id"`
	Namespace                      string `json:"namespace"`
	Type                           string `json:"type"`
	RunID                          string `json:"run_id,omitempty"`
	EnvironmentID                  string `json:"environment_id,omitempty"`
	Reason                         string `json:"reason"`
	RequestedCPUMilli              int64  `json:"requested_cpu_milli"`
	ReservedCPUMilli               int64  `json:"reserved_cpu_milli"`
	CPUMilliLimit                  *int64 `json:"cpu_milli_limit,omitempty"`
	AvailableCPUMilli              *int64 `json:"available_cpu_milli,omitempty"`
	RequestedMemoryBytes           int64  `json:"requested_memory_bytes"`
	ReservedMemoryBytes            int64  `json:"reserved_memory_bytes"`
	MemoryBytesLimit               *int64 `json:"memory_bytes_limit,omitempty"`
	AvailableMemoryBytes           *int64 `json:"available_memory_bytes,omitempty"`
	RequestedEphemeralStorageBytes int64  `json:"requested_ephemeral_storage_bytes"`
	ReservedEphemeralStorageBytes  int64  `json:"reserved_ephemeral_storage_bytes"`
	EphemeralStorageBytesLimit     *int64 `json:"ephemeral_storage_bytes_limit,omitempty"`
	AvailableEphemeralStorageBytes *int64 `json:"available_ephemeral_storage_bytes,omitempty"`
	CreatedAt                      string `json:"created_at,omitempty"`
}

type NamespaceQuotaJSON struct {
	Namespace                      string `json:"namespace"`
	CPUMilliLimit                  *int64 `json:"cpu_milli_limit,omitempty"`
	MemoryBytesLimit               *int64 `json:"memory_bytes_limit,omitempty"`
	ReservedCPUMilli               int64  `json:"reserved_cpu_milli"`
	ReservedMemoryBytes            int64  `json:"reserved_memory_bytes"`
	AvailableCPUMilli              *int64 `json:"available_cpu_milli,omitempty"`
	AvailableMemoryBytes           *int64 `json:"available_memory_bytes,omitempty"`
	EphemeralStorageBytesLimit     *int64 `json:"ephemeral_storage_bytes_limit,omitempty"`
	ReservedEphemeralStorageBytes  int64  `json:"reserved_ephemeral_storage_bytes"`
	AvailableEphemeralStorageBytes *int64 `json:"available_ephemeral_storage_bytes,omitempty"`
	CreatedAt                      string `json:"created_at,omitempty"`
	UpdatedAt                      string `json:"updated_at,omitempty"`
}

func PrintNamespaceQuotaJSON(w io.Writer, quota *quotav1.NamespaceQuota) error {
	return PrintJSON(w, NamespaceQuotaResponseJSON{Quota: NewNamespaceQuotaJSON(quota)})
}

func PrintNamespaceQuotaListJSON(w io.Writer, resp *quotav1.ListNamespaceQuotasResponse) error {
	out := NamespaceQuotaListJSON{}
	if resp != nil {
		out.Quotas = make([]*NamespaceQuotaJSON, 0, len(resp.GetQuotas()))
		for _, quota := range resp.GetQuotas() {
			out.Quotas = append(out.Quotas, NewNamespaceQuotaJSON(quota))
		}
	}
	return PrintJSON(w, out)
}

func PrintNamespaceQuotaEventsJSON(w io.Writer, resp *quotav1.ListNamespaceQuotaEventsResponse) error {
	out := NamespaceQuotaEventsJSON{}
	if resp != nil {
		out.Events = make([]*NamespaceQuotaEventJSON, 0, len(resp.GetEvents()))
		for _, event := range resp.GetEvents() {
			out.Events = append(out.Events, NewNamespaceQuotaEventJSON(event))
		}
	}
	return PrintJSON(w, out)
}

func NewNamespaceQuotaEventJSON(event *quotav1.NamespaceQuotaEvent) *NamespaceQuotaEventJSON {
	if event == nil {
		return nil
	}
	return &NamespaceQuotaEventJSON{
		ID:                             event.GetID(),
		Namespace:                      event.GetNamespace(),
		Type:                           quotaEventTypeJSON(event.GetType()),
		RunID:                          event.GetRunID(),
		EnvironmentID:                  event.GetEnvironmentID(),
		Reason:                         quotaEventReason(event.GetReason()),
		RequestedCPUMilli:              event.GetRequestedCpuMilli(),
		ReservedCPUMilli:               event.GetReservedCpuMilli(),
		CPUMilliLimit:                  optionalWrapperInt64(event.GetCpuMilliLimit()),
		AvailableCPUMilli:              optionalWrapperInt64(event.GetAvailableCpuMilli()),
		RequestedMemoryBytes:           event.GetRequestedMemoryBytes(),
		ReservedMemoryBytes:            event.GetReservedMemoryBytes(),
		MemoryBytesLimit:               optionalWrapperInt64(event.GetMemoryBytesLimit()),
		AvailableMemoryBytes:           optionalWrapperInt64(event.GetAvailableMemoryBytes()),
		RequestedEphemeralStorageBytes: event.GetRequestedEphemeralStorageBytes(),
		ReservedEphemeralStorageBytes:  event.GetReservedEphemeralStorageBytes(),
		EphemeralStorageBytesLimit:     optionalWrapperInt64(event.GetEphemeralStorageBytesLimit()),
		AvailableEphemeralStorageBytes: optionalWrapperInt64(event.GetAvailableEphemeralStorageBytes()),
		CreatedAt:                      FormatProtoTimestamp(event.GetCreatedAt()),
	}
}

func NewNamespaceQuotaJSON(quota *quotav1.NamespaceQuota) *NamespaceQuotaJSON {
	if quota == nil {
		return nil
	}
	return &NamespaceQuotaJSON{
		Namespace:                      quota.GetNamespace(),
		CPUMilliLimit:                  optionalWrapperInt64(quota.GetCpuMilliLimit()),
		MemoryBytesLimit:               optionalWrapperInt64(quota.GetMemoryBytesLimit()),
		ReservedCPUMilli:               quota.GetReservedCpuMilli(),
		ReservedMemoryBytes:            quota.GetReservedMemoryBytes(),
		AvailableCPUMilli:              optionalWrapperInt64(quota.GetAvailableCpuMilli()),
		AvailableMemoryBytes:           optionalWrapperInt64(quota.GetAvailableMemoryBytes()),
		EphemeralStorageBytesLimit:     optionalWrapperInt64(quota.GetEphemeralStorageBytesLimit()),
		ReservedEphemeralStorageBytes:  quota.GetReservedEphemeralStorageBytes(),
		AvailableEphemeralStorageBytes: optionalWrapperInt64(quota.GetAvailableEphemeralStorageBytes()),
		CreatedAt:                      FormatProtoTimestamp(quota.GetCreatedAt()),
		UpdatedAt:                      FormatProtoTimestamp(quota.GetUpdatedAt()),
	}
}

func quotaEventTypeJSON(value quotav1.NamespaceQuotaEventType) string {
	if value == quotav1.NamespaceQuotaEventType_NAMESPACE_QUOTA_EVENT_TYPE_ADMISSION_REJECTED {
		return "admission-rejected"
	}
	return ""
}

func optionalWrapperInt64(value *wrapperspb.Int64Value) *int64 {
	if value == nil {
		return nil
	}
	out := value.GetValue()
	return &out
}
