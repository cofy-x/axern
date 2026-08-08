package runtime

import (
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/stretchr/testify/require"
)

func TestResolveWritableLayerDefaultsRequestAndLimit(t *testing.T) {
	in := &apipb.CreateContainerRequest{Rootfs: &apipb.Rootfs{Readonly: false}}
	got, err := resolveWritableLayer(in, 256<<20)
	require.NoError(t, err)
	require.EqualValues(t, 256<<20, got.GetWritableLayerRequestBytes())
	require.EqualValues(t, 256<<20, got.GetWritableLayerLimitBytes())
	require.Zero(t, in.GetWritableLayerLimitBytes(), "input request must remain immutable")
}

func TestResolveWritableLayerRejectsReadonlyResources(t *testing.T) {
	_, err := resolveWritableLayer(&apipb.CreateContainerRequest{
		Rootfs: &apipb.Rootfs{Readonly: true}, WritableLayerLimitBytes: 1,
	}, 256<<20)
	require.ErrorContains(t, err, "readonly rootfs conflicts")
}

func TestResolveWritableLayerRejectsRequestAboveLimit(t *testing.T) {
	_, err := resolveWritableLayer(&apipb.CreateContainerRequest{
		Rootfs: &apipb.Rootfs{}, WritableLayerRequestBytes: 2, WritableLayerLimitBytes: 1,
	}, 256<<20)
	require.ErrorContains(t, err, "request must be <= limit")
}
