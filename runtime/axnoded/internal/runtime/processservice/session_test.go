package processservice

import (
	"context"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenProcessMapsRequestToExecSession(t *testing.T) {
	var gotRequest *apipb.ExecSessionOpen
	var gotOptions contract.HandlerOptions
	service := New(func(_ context.Context, request *apipb.ExecSessionOpen, options contract.HandlerOptions) (contract.Session, error) {
		gotRequest = request
		gotOptions = options
		return nil, nil
	})

	options := contract.HandlerOptions{ContainerID: "container-id"}
	_, err := service.OpenProcess(t.Context(), &apipb.ProcessOpen{
		ID:      "process-id",
		Command: []string{"/bin/sh", "-c", "echo ok"},
		Tty:     true,
		Env: map[string]string{
			"B": "2",
			"A": "1",
		},
		Cwd:  "/work",
		User: "1000",
	}, options)
	require.NoError(t, err)

	require.NotNil(t, gotRequest)
	assert.Equal(t, "process-id", gotRequest.GetID())
	assert.Equal(t, []string{"/bin/sh", "-c", "echo ok"}, gotRequest.GetCommand())
	assert.True(t, gotRequest.GetTty())
	assert.Equal(t, "/work", gotRequest.GetCwd())
	assert.Equal(t, "1000", gotRequest.GetUser())
	assert.Equal(t, []*apipb.KeyValue{{Key: "A", Value: "1"}, {Key: "B", Value: "2"}}, gotRequest.GetEnvs())
	assert.Equal(t, options.ContainerID, gotOptions.ContainerID)
}

func TestProcessKeyValuesSortsKeys(t *testing.T) {
	items := processKeyValues(map[string]string{
		"B": "2",
		"A": "1",
	})

	require.Len(t, items, 2)
	require.Equal(t, "A", items[0].GetKey())
	require.Equal(t, "1", items[0].GetValue())
	require.Equal(t, "B", items[1].GetKey())
	require.Equal(t, "2", items[1].GetValue())
}
