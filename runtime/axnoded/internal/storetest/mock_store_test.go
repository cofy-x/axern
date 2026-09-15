package storetest

import (
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/stretchr/testify/assert"
)

func TestMockStore(t *testing.T) {
	mockDB := NewMockStore()
	fooData := &apipb.ContainerMetadata{Stdout: "value1", Stderr: "value2"}
	assert.NoError(t, mockDB.SaveSnapshot("foo", fooData))
	var missing apipb.ContainerMetadata
	assert.ErrorIs(t, mockDB.LoadSnapshot("missing", &missing), errord.ErrNotFound)
	var got apipb.ContainerMetadata
	assert.NoError(t, mockDB.LoadSnapshot("foo", &got))
	assert.Equal(t, fooData.String(), got.String())
	assert.Error(t, mockDB.SaveSnapshot("bar", &apipb.NetworkLedger{Leases: []*apipb.NetworkLease{{AllocationID: "failed"}}}))
	assert.NoError(t, mockDB.DeleteRecord("foo", "state"))
	assert.ErrorIs(t, mockDB.LoadSnapshot("foo", &got), errord.ErrNotFound)
}
