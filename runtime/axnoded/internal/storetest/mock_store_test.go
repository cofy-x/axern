package storetest

import (
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"github.com/stretchr/testify/assert"
)

func TestMockStore(t *testing.T) {
	mockDB := NewMockStore()
	fooData := &apipb.Map{Items: map[string]string{"key1": "value1", "key2": "value2"}}
	assert.NoError(t, mockDB.SaveSnapshot("foo", fooData))
	var missing apipb.Map
	assert.ErrorIs(t, mockDB.LoadSnapshot("missing", &missing), errord.ErrNotFound)
	var got apipb.Map
	assert.NoError(t, mockDB.LoadSnapshot("foo", &got))
	assert.Equal(t, fooData.String(), got.String())
	assert.Error(t, mockDB.SaveSnapshot("bar", &apipb.Slice{Items: []string{"failed"}}))
	assert.NoError(t, mockDB.DeleteRecord("foo", "state"))
	assert.ErrorIs(t, mockDB.LoadSnapshot("foo", &got), errord.ErrNotFound)
}
