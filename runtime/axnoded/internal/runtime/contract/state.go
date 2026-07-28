package contract

type ContainerStatus string

const (
	ContainerStatusCreated ContainerStatus = "created"
	ContainerStatusRunning ContainerStatus = "running"
	ContainerStatusExited  ContainerStatus = "stopped"
	ContainerStatusUnknown ContainerStatus = "unknown"
)

// UnionContainerState is the state of a container returned by a runtime list command.
type UnionContainerState struct {
	ID             string          `json:"id"`
	InitProcessPid int             `json:"pid"`
	Status         ContainerStatus `json:"status"`
	Bundle         string          `json:"bundle"`
	Created        string          `json:"created"`
}
