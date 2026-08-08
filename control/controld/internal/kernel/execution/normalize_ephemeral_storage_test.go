package executionkernel

import (
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestNormalizeConfigForWritableRootfsResolvesEphemeralStorageContract(t *testing.T) {
	config, err := NormalizeConfigForRootfs(&commonv1.ExecutionConfig{}, false)
	require.NoError(t, err)
	assert.Equal(t, DefaultEphemeralStorageBytes, config.GetResources().GetRequests().GetEphemeralStorageBytes())
	assert.Equal(t, DefaultEphemeralStorageBytes, config.GetResources().GetLimits().GetEphemeralStorageBytes())

	config, err = NormalizeConfigForRootfs(&commonv1.ExecutionConfig{Resources: &commonv1.ResourceSpec{
		Limits: &commonv1.ResourceQuantity{EphemeralStorageBytes: 1234},
	}}, false)
	require.NoError(t, err)
	assert.Equal(t, int64(1234), config.GetResources().GetRequests().GetEphemeralStorageBytes())
	assert.Equal(t, int64(1234), config.GetResources().GetLimits().GetEphemeralStorageBytes())
}

func TestNormalizeConfigForRootfsRejectsEphemeralStorageConflicts(t *testing.T) {
	_, err := NormalizeConfigForRootfs(&commonv1.ExecutionConfig{Resources: &commonv1.ResourceSpec{
		Requests: &commonv1.ResourceQuantity{EphemeralStorageBytes: 1},
	}}, true)
	assert.Equal(t, codes.InvalidArgument, grpcstatus.Code(err))

	_, err = NormalizeConfigForRootfs(&commonv1.ExecutionConfig{Resources: &commonv1.ResourceSpec{
		Requests: &commonv1.ResourceQuantity{EphemeralStorageBytes: -1},
		Limits:   &commonv1.ResourceQuantity{EphemeralStorageBytes: 10},
	}}, false)
	assert.Equal(t, codes.InvalidArgument, grpcstatus.Code(err))

	_, err = NormalizeConfigForRootfs(&commonv1.ExecutionConfig{Resources: &commonv1.ResourceSpec{
		Requests: &commonv1.ResourceQuantity{EphemeralStorageBytes: 11},
		Limits:   &commonv1.ResourceQuantity{EphemeralStorageBytes: 10},
	}}, false)
	assert.Equal(t, codes.InvalidArgument, grpcstatus.Code(err))
}
