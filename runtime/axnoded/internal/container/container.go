package container

import (
	"strings"
	"time"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

const MaxContainerNum = 1000

// Container contains all resources associated with the container. All methods to
// mutate the internal state are thread-safe.
type Container struct {
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

func (c *Container) EnvValue(key string) string {
	if c.Spec == nil || c.Spec.Process == nil {
		return ""
	}
	for _, env := range c.Spec.Process.Env {
		s := strings.Split(env, "=")
		if len(s) == 2 && s[0] == key {
			return s[1]
		}
	}
	return ""
}

func (c *Container) ApiStatus() *runtimeapi.ContainerStatus {
	if c.Status == nil || c.Spec == nil || c.Metadata == nil || c.Spec.Process == nil {
		return &runtimeapi.ContainerStatus{}
	}
	envKv := make([]*runtimeapi.KeyValue, 0)
	for _, env := range c.Spec.Process.Env {
		if len(strings.Split(env, "=")) != 2 {
			continue
		}
		envKv = append(envKv, &runtimeapi.KeyValue{
			Key:   strings.Split(env, "=")[0],
			Value: strings.Split(env, "=")[1],
		})
	}

	copyLabels := make(map[string]string)
	if c.Metadata.Labels != nil {
		for k, v := range c.Metadata.Labels {
			copyLabels[k] = v
		}
	}

	var copyResource *runtimeapi.LinuxContainerResources
	resource := c.Status.Get().LinuxResources
	if resource != nil {
		copyResourcesUnified := make(map[string]string)
		if resource.Unified != nil {
			copyResourcesUnified = make(map[string]string, len(resource.Unified))
			for k, v := range resource.Unified {
				copyResourcesUnified[k] = v
			}
		}

		copyHugePageLimits := make([]*runtimeapi.HugepageLimit, len(resource.HugepageLimits))
		for idx := range resource.HugepageLimits {
			copyHugePageLimits[idx] = &runtimeapi.HugepageLimit{
				PageSize: resource.HugepageLimits[idx].PageSize,
				Limit:    resource.HugepageLimits[idx].Limit,
			}
		}
		copyResource = &runtimeapi.LinuxContainerResources{
			CpuPeriod:              resource.CpuPeriod,
			CpuQuota:               resource.CpuQuota,
			CpuShares:              resource.CpuShares,
			MemoryLimitInBytes:     resource.MemoryLimitInBytes,
			OomScoreAdj:            resource.OomScoreAdj,
			CpusetCpus:             resource.CpusetCpus,
			CpusetMems:             resource.CpusetMems,
			HugepageLimits:         copyHugePageLimits,
			Unified:                copyResourcesUnified,
			MemorySwapLimitInBytes: resource.MemorySwapLimitInBytes,
		}
	}

	return &runtimeapi.ContainerStatus{
		ID:             c.Metadata.ID,
		Command:        c.Spec.Process.Args,
		Runtime:        c.Metadata.RuntimeHandler,
		State:          c.Status.Get().State(),
		StartedAt:      ParseTimestamp(c.Status.Get().StartedAt),
		FinishedAt:     ParseTimestamp(c.Status.Get().FinishedAt),
		ExitCode:       c.Status.Get().ExitCode,
		Message:        c.Status.Get().Message,
		DiagnosticCode: c.Status.Get().DiagnosticCode,
		Labels:         copyLabels,
		Mounts:         MountsToAPI(c.Spec.Mounts),
		Envs:           envKv,
		Stdout:         c.Metadata.Stdout,
		Stderr:         c.Metadata.Stderr,
		LinuxResources: copyResource,
		Pid:            int32(c.Status.Get().Pid),
	}
}
