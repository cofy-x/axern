package executionkernel

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestNormalizeConfigForWritableRootfsResolvesWritableLayerContract(t *testing.T) {
	config, err := NormalizeConfigForRootfs(&commonv1.ExecutionConfig{}, false)
	require.NoError(t, err)
	assert.Equal(t, DefaultWritableLayerBytes, config.GetResources().GetRequests().GetWritableLayerBytes())
	assert.Equal(t, DefaultWritableLayerBytes, config.GetResources().GetLimits().GetWritableLayerBytes())

	config, err = NormalizeConfigForRootfs(&commonv1.ExecutionConfig{Resources: &commonv1.ResourceSpec{
		Limits: &commonv1.ResourceQuantity{WritableLayerBytes: 1234},
	}}, false)
	require.NoError(t, err)
	assert.Equal(t, int64(1234), config.GetResources().GetRequests().GetWritableLayerBytes())
	assert.Equal(t, int64(1234), config.GetResources().GetLimits().GetWritableLayerBytes())
}

func TestNormalizeConfigForRootfsRejectsWritableLayerConflicts(t *testing.T) {
	_, err := NormalizeConfigForRootfs(&commonv1.ExecutionConfig{Resources: &commonv1.ResourceSpec{
		Requests: &commonv1.ResourceQuantity{WritableLayerBytes: 1},
	}}, true)
	assert.Equal(t, codes.InvalidArgument, grpcstatus.Code(err))

	_, err = NormalizeConfigForRootfs(&commonv1.ExecutionConfig{Resources: &commonv1.ResourceSpec{
		Requests: &commonv1.ResourceQuantity{WritableLayerBytes: -1},
		Limits:   &commonv1.ResourceQuantity{WritableLayerBytes: 10},
	}}, false)
	assert.Equal(t, codes.InvalidArgument, grpcstatus.Code(err))

	_, err = NormalizeConfigForRootfs(&commonv1.ExecutionConfig{Resources: &commonv1.ResourceSpec{
		Requests: &commonv1.ResourceQuantity{WritableLayerBytes: 11},
		Limits:   &commonv1.ResourceQuantity{WritableLayerBytes: 10},
	}}, false)
	assert.Equal(t, codes.InvalidArgument, grpcstatus.Code(err))
}
