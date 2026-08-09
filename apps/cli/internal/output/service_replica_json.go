package output

import (
	"io"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

type ServiceReplicaListJSON struct {
	Replicas []*ServiceReplicaJSON `json:"replicas"`
}

type ServiceReplicaJSON struct {
	ID                   string                            `json:"id"`
	ServiceID            string                            `json:"service_id"`
	NodeID               string                            `json:"node_id,omitempty"`
	Attempt              int64                             `json:"attempt"`
	Status               string                            `json:"status"`
	CreatedAt            string                            `json:"created_at,omitempty"`
	UpdatedAt            string                            `json:"updated_at,omitempty"`
	ExitCode             *int32                            `json:"exit_code,omitempty"`
	ExitCodeKnown        bool                              `json:"exit_code_known,omitempty"`
	Ended                bool                              `json:"ended,omitempty"`
	Outdated             bool                              `json:"outdated,omitempty"`
	DiagnosticCode       string                            `json:"diagnostic_code"`
	Ready                bool                              `json:"ready"`
	ReadinessMessage     string                            `json:"readiness_message,omitempty"`
	Message              string                            `json:"message,omitempty"`
	LifecycleRetry       *ServiceReplicaLifecycleRetryJSON `json:"lifecycle_retry,omitempty"`
	CapabilityConditions []*CapabilityConditionJSON        `json:"capability_conditions,omitempty"`
}

type ServiceReplicaLifecycleRetryJSON struct {
	Reason    string `json:"reason"`
	Attempts  int32  `json:"attempts"`
	LastError string `json:"last_error,omitempty"`
	NextRunAt string `json:"next_run_at,omitempty"`
}

func PrintServiceReplicaListJSON(w io.Writer, resp *servicev1.ListServiceReplicasResponse) error {
	out := ServiceReplicaListJSON{}
	if resp != nil {
		out.Replicas = make([]*ServiceReplicaJSON, 0, len(resp.GetReplicas()))
		for _, replica := range resp.GetReplicas() {
			out.Replicas = append(out.Replicas, NewServiceReplicaJSON(replica))
		}
	}
	return PrintJSON(w, out)
}

func NewServiceReplicaJSON(replica *servicev1.ServiceReplica) *ServiceReplicaJSON {
	if replica == nil {
		return nil
	}
	return &ServiceReplicaJSON{
		ID:                   replica.GetID(),
		ServiceID:            replica.GetServiceID(),
		NodeID:               replica.GetNodeID(),
		Attempt:              replica.GetAttempt(),
		Status:               AllocationStatusLabel(replica.GetStatus()),
		CreatedAt:            FormatProtoTimestamp(replica.GetCreatedAt()),
		UpdatedAt:            FormatProtoTimestamp(replica.GetUpdatedAt()),
		ExitCode:             knownExitCode(replica.GetExitCode(), replica.GetExitCodeKnown()),
		ExitCodeKnown:        replica.GetExitCodeKnown(),
		Ended:                replica.GetEnded(),
		Outdated:             replica.GetOutdated(),
		DiagnosticCode:       WorkloadDiagnosticCodeLabel(replica.GetDiagnosticCode()),
		Ready:                replica.GetReady(),
		ReadinessMessage:     replica.GetReadinessMessage(),
		Message:              replica.GetMessage(),
		LifecycleRetry:       newServiceReplicaLifecycleRetryJSON(replica.GetLifecycleRetry()),
		CapabilityConditions: newCapabilityConditionJSONs(replica.GetCapabilityConditions()),
	}
}

func newServiceReplicaLifecycleRetryJSON(retry *servicev1.ServiceReplicaLifecycleRetry) *ServiceReplicaLifecycleRetryJSON {
	if retry == nil {
		return nil
	}
	return &ServiceReplicaLifecycleRetryJSON{
		Reason:    serviceReplicaLifecycleRetryReasonLabel(retry.GetReason()),
		Attempts:  retry.GetAttempts(),
		LastError: retry.GetLastError(),
		NextRunAt: FormatProtoTimestamp(retry.GetNextRunAt()),
	}
}
