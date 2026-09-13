package startplan

import (
	"testing"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/protobuf/proto"
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
		assert.Equal(t, res.MemoryLimitInBytes, res.MemorySwapLimitInBytes)
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
		EnvironmentTemplate: &runtime.EnvironmentTemplate{},
	}))
	valid := &runtime.StartRequest{
		AllocationID: "alloc-valid",
		EnvironmentTemplate: &runtime.EnvironmentTemplate{
			Rootfs: &runtime.RootfsConfig{},
		},
	}
	assert.NoError(t, ValidateStartRequest(valid))
	invalidRegistry := proto.Clone(valid).(*runtime.StartRequest)
	invalidRegistry.RegistryCredential = &runtime.RegistryCredential{DockerConfigJson: "{"}
	assert.Error(t, ValidateStartRequest(invalidRegistry))
	nonObjectRegistry := proto.Clone(valid).(*runtime.StartRequest)
	nonObjectRegistry.RegistryCredential = &runtime.RegistryCredential{DockerConfigJson: "[]"}
	assert.Error(t, ValidateStartRequest(nonObjectRegistry))
	invalidSecret := proto.Clone(valid).(*runtime.StartRequest)
	invalidSecret.SecretFiles = []*runtime.ResolvedSecretFile{{Path: "../secret"}}
	assert.Error(t, ValidateStartRequest(invalidSecret))
	invalidPort := proto.Clone(valid).(*runtime.StartRequest)
	invalidPort.Ports = []*commonv1.PortSpec{{ContainerPort: 70000}}
	assert.Error(t, ValidateStartRequest(invalidPort))
	invalidMount := proto.Clone(valid).(*runtime.StartRequest)
	invalidMount.Mounts = []*runtime.Mount{{Target: "../workspace"}}
	assert.Error(t, ValidateStartRequest(invalidMount))
	invalidImageMount := proto.Clone(valid).(*runtime.StartRequest)
	invalidImageMount.ImageMounts = []*runtime.ImageMount{nil}
	assert.Error(t, ValidateStartRequest(invalidImageMount))
}

func TestEffectiveNetworkMode(t *testing.T) {
	assert.Equal(t, "bridge", EffectiveNetworkMode("bridge", &runtime.StartRequest{}))
	assert.Equal(t, "host", EffectiveNetworkMode("bridge", &runtime.StartRequest{
		Network: &commonv1.NetworkSpec{Mode: commonv1.NetworkMode_NETWORK_MODE_HOST},
	}))
}
