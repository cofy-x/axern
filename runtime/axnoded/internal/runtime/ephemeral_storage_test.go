package runtime

import (
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/stretchr/testify/require"
)

func TestResolveEphemeralStorageDefaultsRequestAndLimit(t *testing.T) {
	in := &apipb.CreateContainerRequest{Rootfs: &apipb.Rootfs{Readonly: false}}
	got, err := resolveEphemeralStorage(in, 256<<20)
	require.NoError(t, err)
	require.EqualValues(t, 256<<20, got.GetEphemeralStorageRequestBytes())
	require.EqualValues(t, 256<<20, got.GetEphemeralStorageLimitBytes())
	require.Zero(t, in.GetEphemeralStorageLimitBytes(), "input request must remain immutable")
}

func TestResolveEphemeralStorageRejectsReadonlyResources(t *testing.T) {
	_, err := resolveEphemeralStorage(&apipb.CreateContainerRequest{
		Rootfs: &apipb.Rootfs{Readonly: true}, EphemeralStorageLimitBytes: 1,
	}, 256<<20)
	require.ErrorContains(t, err, "readonly rootfs conflicts")
}

func TestResolveEphemeralStorageRejectsRequestAboveLimit(t *testing.T) {
	_, err := resolveEphemeralStorage(&apipb.CreateContainerRequest{
		Rootfs: &apipb.Rootfs{}, EphemeralStorageRequestBytes: 2, EphemeralStorageLimitBytes: 1,
	}, 256<<20)
	require.ErrorContains(t, err, "request must be <= limit")
}
