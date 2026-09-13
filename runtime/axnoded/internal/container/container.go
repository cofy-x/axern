package container

import (
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

const MaxContainerNum = 1000

// Container contains all resources associated with the container. All methods to
// mutate the internal state are thread-safe.
type Container struct {
	// ID is supplied by the container directory/map key. It is never decoded
	// from runtime metadata, OCI labels, or annotations.
	ID string
	// metadata stores the metadata of the container. Can not be modified.
	Metadata *apipb.ContainerMetadata
	// Status stores the status of the container.
	Status StatusStorage
	// PATH is the path to the container's data. Under this path, there is a config.json and metadata.pb file.
	Spec *spec.Spec
	PATH string
}

type EventType string

const (
	EventTypeExit   EventType = "exit"
	EventTypeDelete EventType = "delete"
)

type Event struct {
	Type        EventType `json:"type"`
	ContainerID string    `json:"id"`
	// lifecycle information
	Pid            int32                           `json:"pid"`
	ExitedAt       time.Time                       `json:"exited_at"`
	ExitCode       int32                           `json:"exit_code"`
	ExitCodeKnown  bool                            `json:"exit_code_known"`
	Reason         string                          `json:"reason"`
	DiagnosticCode commonv1.WorkloadDiagnosticCode `json:"diagnostic_code"`
}
