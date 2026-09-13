package container

import (
	"fmt"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
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

// Status is the status of a container.
type Status struct {
	// RuntimeState is the explicit local lifecycle fact. Timestamps describe
	// the transition when available; their absence never changes the state.
	RuntimeState apipb.RuntimeCheckpointState
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
}

// Equal compares two Status values for equality without reflection.
func (s Status) Equal(other Status) bool {
	if s.RuntimeState != other.RuntimeState || s.Pid != other.Pid || s.StartedAt != other.StartedAt ||
		s.FinishedAt != other.FinishedAt || s.ExitCode != other.ExitCode ||
		s.ExitCodeKnown != other.ExitCodeKnown || s.Message != other.Message ||
		s.DiagnosticCode != other.DiagnosticCode {
		return false
	}
	return true
}

// State returns current state of the container based on the container status.
func (s Status) State() apipb.ContainerState {
	switch s.RuntimeState {
	case apipb.RuntimeCheckpointState_RUNTIME_CHECKPOINT_STATE_RUNNING:
		return apipb.ContainerState_CONTAINER_RUNNING
	case apipb.RuntimeCheckpointState_RUNTIME_CHECKPOINT_STATE_EXITED:
		return apipb.ContainerState_CONTAINER_EXITED
	default:
		return apipb.ContainerState_CONTAINER_UNKNOWN
	}
}

// encode persists only the lifecycle checkpoint. Allocation specification,
// admission, and resource enforcement have separate authoritative records.
func (s *Status) encode() ([]byte, error) {
	startedAt, err := checkpointTimestamp(s.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid started timestamp: %w", err)
	}
	finishedAt, err := checkpointTimestamp(s.FinishedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid finished timestamp: %w", err)
	}
	return proto.Marshal(&apipb.RuntimeCheckpoint{
		State:          s.RuntimeState,
		InitProcessPid: int32(s.Pid),
		StartedAt:      startedAt,
		FinishedAt:     finishedAt,
		ExitCode:       s.ExitCode,
		ExitCodeKnown:  s.ExitCodeKnown,
		Message:        s.Message,
		DiagnosticCode: s.DiagnosticCode,
	})
}

// decode decodes Status from bytes.
func (s *Status) decode(data []byte) error {
	checkpoint := &apipb.RuntimeCheckpoint{}
	if err := proto.Unmarshal(data, checkpoint); err != nil {
		return err
	}
	if checkpoint.GetStartedAt() != nil {
		if err := checkpoint.GetStartedAt().CheckValid(); err != nil {
			return fmt.Errorf("invalid started timestamp: %w", err)
		}
	}
	if checkpoint.GetFinishedAt() != nil {
		if err := checkpoint.GetFinishedAt().CheckValid(); err != nil {
			return fmt.Errorf("invalid finished timestamp: %w", err)
		}
	}
	*s = Status{
		RuntimeState:   checkpoint.GetState(),
		Pid:            int(checkpoint.GetInitProcessPid()),
		StartedAt:      checkpointTimestampString(checkpoint.GetStartedAt()),
		FinishedAt:     checkpointTimestampString(checkpoint.GetFinishedAt()),
		ExitCode:       checkpoint.GetExitCode(),
		ExitCodeKnown:  checkpoint.GetExitCodeKnown(),
		Message:        checkpoint.GetMessage(),
		DiagnosticCode: checkpoint.GetDiagnosticCode(),
	}
	return nil
}

func checkpointTimestamp(value string) (*timestamppb.Timestamp, error) {
	if value == "" || value == "0" {
		return nil, nil
	}
	parsed := ParseTimestampTime(value)
	if parsed.IsZero() {
		return nil, fmt.Errorf("cannot parse %q", value)
	}
	timestamp := timestamppb.New(parsed)
	if err := timestamp.CheckValid(); err != nil {
		return nil, err
	}
	return timestamp, nil
}

func checkpointTimestampString(value *timestamppb.Timestamp) string {
	if value == nil {
		return ""
	}
	return value.AsTime().UTC().Format(time.RFC3339Nano)
}
