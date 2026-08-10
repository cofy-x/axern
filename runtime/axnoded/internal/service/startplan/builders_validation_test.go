package startplan

import (
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimecore "github.com/cofy-x/axern/runtime/axnoded/internal/runtime"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/workloadidentity"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/stretchr/testify/assert"
)

func TestResourcesToLinux(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		res := ResourcesToLinux(nil)
		assert.Equal(t, uint64(512), res.CpuShares)
		assert.Equal(t, int64(0), res.MemoryLimitInBytes)
	})

	t.Run("converts requests and limits", func(t *testing.T) {
		res := ResourcesToLinux(&commonv1.ResourceSpec{
			Requests: &commonv1.ResourceQuantity{CpuMilli: 1000},
			Limits:   &commonv1.ResourceQuantity{CpuMilli: 500, MemoryBytes: 256 * 1024 * 1024},
		})
		assert.Equal(t, uint64(1024), res.CpuShares)
		assert.Equal(t, uint64(100000), res.CpuPeriod)
		assert.Equal(t, int64(50000), res.CpuQuota)
		assert.Equal(t, int64(256*1024*1024), res.MemoryLimitInBytes)
	})

	t.Run("enforces minimum cpu shares", func(t *testing.T) {
		res := ResourcesToLinux(&commonv1.ResourceSpec{
			Requests: &commonv1.ResourceQuantity{CpuMilli: 1},
		})
		assert.Equal(t, uint64(2), res.CpuShares)
	})
}

func TestValidateStartRequest(t *testing.T) {
	assert.Error(t, ValidateStartRequest(nil))
	assert.Error(t, ValidateStartRequest(&runtime.StartRequest{}))
	assert.Error(t, ValidateStartRequest(&runtime.StartRequest{
		RuntimeTemplate: &runtime.RuntimeTemplate{},
	}))
	assert.NoError(t, ValidateStartRequest(&runtime.StartRequest{
		RuntimeTemplate: &runtime.RuntimeTemplate{
			Rootfs: &runtime.RootfsConfig{},
		},
	}))
}

func TestBuildStartLabels(t *testing.T) {
	request := &runtime.StartRequest{
		RuntimeTemplate: &runtime.RuntimeTemplate{ID: "rt-1"},
	}

	t.Run("default label only", func(t *testing.T) {
		labels := BuildStartLabels(request)
		assert.Equal(t, map[string]string{
			workloadidentity.LabelKeyRuntimeID: "rt-1",
		}, labels)
	})

	t.Run("allocation identity", func(t *testing.T) {
		req := &runtime.StartRequest{
			RuntimeTemplate: &runtime.RuntimeTemplate{ID: "rt-1"},
			ContainerID:     "alloc-1234567890abcdef",
		}
		labels := BuildStartLabels(req)
		assert.Equal(t, "alloc-1234567890abcdef", labels[workloadidentity.LabelKeyAllocationID])
	})

	t.Run("block network", func(t *testing.T) {
		req := &runtime.StartRequest{
			RuntimeTemplate: &runtime.RuntimeTemplate{ID: "rt-1"},
			ExtraConfig:     `{"blockNetwork":true}`,
		}
		labels := BuildStartLabels(req)
		assert.Equal(t, "rt-1", labels[workloadidentity.LabelKeyRuntimeID])
		assert.Equal(t, config.NetAcBlockAll, labels["netac-rules"])
	})

	t.Run("cidr allowlist", func(t *testing.T) {
		req := &runtime.StartRequest{
			RuntimeTemplate: &runtime.RuntimeTemplate{ID: "rt-1"},
			ExtraConfig:     `{"cidrAllowlist":"10.0.0.0/8"}`,
		}
		labels := BuildStartLabels(req)
		assert.Equal(t, "10.0.0.0/8", labels["netac-rules"])
	})

	t.Run("invalid extra config falls back", func(t *testing.T) {
		req := &runtime.StartRequest{
			RuntimeTemplate: &runtime.RuntimeTemplate{ID: "rt-1"},
			ExtraConfig:     `{"blockNetwork":`,
		}
		labels := BuildStartLabels(req)
		assert.Equal(t, map[string]string{
			workloadidentity.LabelKeyRuntimeID: "rt-1",
		}, labels)
	})

	t.Run("linux capabilities normalized and deduplicated", func(t *testing.T) {
		req := &runtime.StartRequest{
			RuntimeTemplate: &runtime.RuntimeTemplate{ID: "rt-1"},
			ExtraConfig:     `{"linuxCapabilities":["cap_net_raw"," CAP_NET_BIND_SERVICE ","cap_net_raw",""]}`,
		}
		labels := BuildStartLabels(req)
		assert.Equal(t, "CAP_NET_RAW,CAP_NET_BIND_SERVICE", labels[runtimecore.LabelKeyLinuxCapabilities])
	})

	t.Run("workload identity", func(t *testing.T) {
		req := &runtime.StartRequest{
			RuntimeTemplate:   &runtime.RuntimeTemplate{ID: "rt-1"},
			ExtraConfig:       `{"namespace":"team-a","serviceId":"claude-code"}`,
			AllocationAttempt: 3,
		}
		labels := BuildStartLabels(req)
		assert.Equal(t, "team-a", labels[workloadidentity.LabelKeyNamespace])
		assert.Equal(t, "claude-code", labels[workloadidentity.LabelKeyServiceID])
		assert.Equal(t, "3", labels[workloadidentity.LabelKeyAllocationAttempt])
	})
}

func TestEffectiveNetworkMode(t *testing.T) {
	assert.Equal(t, "bridge", EffectiveNetworkMode("bridge", &runtime.StartRequest{}))
	assert.Equal(t, "host", EffectiveNetworkMode("bridge", &runtime.StartRequest{
		Network: "host",
	}))
}
