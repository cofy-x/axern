package container

import (
	"encoding/json"
	"errors"
	"time"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/proto"
)

// The container state machine in the sandbox service:
//
//                         +              +
//                         |              |
//                         | Create       | Load
//                         |              |
//                    +----v----+         |
//                    |         |         |
//                    | Running <---------+-----------+
//                    |         |         |           |
//                    +----+-----         |           |
//                         |              |           |
//                         | Stop/Exit    |           |
//                         |              |           |
//                    +----v----+         |           |
//                    |         <---------+      +----v----+
//                    |  EXITED |                |         |
//                    |         <----------------+ UNKNOWN |
//                    +----+----+       Stop     |         |
//                         |                     +---------+
//                         | Delete
//                         v
//                      DELETED

// statusVersion is current version of container status.
const statusVersion = "v2"

const unknownExitStatusMessage = "container exited but runtime exit status is unavailable"

// versionedStatus is the internal used versioned container status.
type versionedStatus struct {
	// Version indicates the version of the versioned container status.
	Version string
	Status
}

// Status is the status of a container.
type Status struct {
	// Pid is the init process id of the container.
	Pid int
	// StartedAt is the started timestamp.
	StartedAt string
	// FinishedAt is the finished timestamp.
	FinishedAt string
	// ExitCode is the container exit code.
	ExitCode int32
	// ExitCodeKnown reports whether ExitCode came from the runtime and is
	// trustworthy for an exited container.
	ExitCodeKnown bool
	// Message carries lifecycle details such as missing runtime exit status.
	Message string
	// DiagnosticCode is the structured terminal reason proven before the exit
	// checkpoint is published. It survives reporter retries and node restart.
	DiagnosticCode commonv1.WorkloadDiagnosticCode
	// Unknown indicates that the container status is not fully loaded.
	// This field doesn't need to be checkpointed.
	Unknown bool `json:"-"`
	// ResourceSpec keeps the scheduler-facing request/limit contract.
	ResourceSpec *commonv1.ResourceSpec
	// LinuxResources has the Linux cgroup constraints applied to runc/runsc.
	LinuxResources *runtime.LinuxContainerResources
}

// Equal compares two Status values for equality without reflection.
func (s Status) Equal(other Status) bool {
	if s.Pid != other.Pid || s.StartedAt != other.StartedAt ||
		s.FinishedAt != other.FinishedAt || s.ExitCode != other.ExitCode ||
		s.ExitCodeKnown != other.ExitCodeKnown || s.Message != other.Message ||
		s.DiagnosticCode != other.DiagnosticCode ||
		s.Unknown != other.Unknown {
		return false
	}
	if !proto.Equal(s.ResourceSpec, other.ResourceSpec) {
		return false
	}
	if s.LinuxResources == nil && other.LinuxResources == nil {
		return true
	}
	if s.LinuxResources == nil || other.LinuxResources == nil {
		return false
	}
	return s.LinuxResources.CpuPeriod == other.LinuxResources.CpuPeriod &&
		s.LinuxResources.CpuQuota == other.LinuxResources.CpuQuota &&
		s.LinuxResources.CpuShares == other.LinuxResources.CpuShares &&
		s.LinuxResources.CpusetCpus == other.LinuxResources.CpusetCpus &&
		s.LinuxResources.CpusetMems == other.LinuxResources.CpusetMems &&
		s.LinuxResources.MemoryLimitInBytes == other.LinuxResources.MemoryLimitInBytes &&
		s.LinuxResources.MemorySwapLimitInBytes == other.LinuxResources.MemorySwapLimitInBytes &&
		s.LinuxResources.OomScoreAdj == other.LinuxResources.OomScoreAdj
}

// State returns current state of the container based on the container status.
func (s Status) State() runtime.ContainerState {
	if s.Unknown {
		return runtime.ContainerState_CONTAINER_UNKNOWN
	}
	if s.FinishedAt != "" {
		return runtime.ContainerState_CONTAINER_EXITED
	}
	if s.StartedAt != "0" {
		return runtime.ContainerState_CONTAINER_RUNNING
	}
	return runtime.ContainerState_CONTAINER_UNKNOWN
}

// encode encodes Status into bytes in json format.
func (s *Status) encode() ([]byte, error) {
	return json.Marshal(&versionedStatus{
		Version: statusVersion,
		Status:  *s,
	})
}

// decode decodes Status from bytes.
func (s *Status) decode(data []byte) error {
	versioned := &versionedStatus{}
	if err := json.Unmarshal(data, versioned); err != nil {
		return err
	}
	// Handle old version after upgrade.
	switch versioned.Version {
	case statusVersion:
		*s = versioned.Status
		return nil
	}
	return errors.New("unsupported version")
}

func GenerateStatusFromState(state *contract.UnionContainerState, path string) StatusStorage {
	startedAt := state.Created
	if startedAt == "" {
		startedAt = time.Now().Format(time.RFC3339)
	}

	s := &statusStorage{
		status: Status{
			Pid:            state.InitProcessPid,
			StartedAt:      startedAt,
			FinishedAt:     "",
			ExitCode:       0,
			ExitCodeKnown:  false,
			Message:        "",
			DiagnosticCode: commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED,
			Unknown:        false,
			LinuxResources: nil,
		},
		path: path,
	}

	// means axnoded didn't catch the exit event
	if state.Status != contract.ContainerStatusRunning {
		s.status.FinishedAt = time.Now().Format(time.RFC3339)
		s.status.ExitCode = -1
		s.status.Message = unknownExitStatusMessage
	}
	return s
}

func UpdateStatusByState(state *contract.UnionContainerState, status Status) Status {
	if state == nil {
		return status
	}
	switch state.Status {
	case contract.ContainerStatusRunning:
		if state.InitProcessPid > 0 {
			status.Pid = state.InitProcessPid
		}
		if state.Created != "" {
			status.StartedAt = state.Created
		}
		status.FinishedAt = ""
		status.ExitCode = -1
		status.ExitCodeKnown = false
		status.Message = ""
		status.DiagnosticCode = commonv1.WorkloadDiagnosticCode_WORKLOAD_DIAGNOSTIC_CODE_UNSPECIFIED
		status.Unknown = false
	case contract.ContainerStatusExited:
		status.Unknown = false
		if state.InitProcessPid > 0 {
			status.Pid = state.InitProcessPid
		}
		if state.Created != "" {
			status.StartedAt = state.Created
		}
		if status.FinishedAt == "" {
			status.FinishedAt = time.Now().Format(time.RFC3339)
		}
		if !status.ExitCodeKnown {
			status.ExitCode = -1
			if status.Message == "" {
				status.Message = unknownExitStatusMessage
			}
		}
	}
	return status
}
