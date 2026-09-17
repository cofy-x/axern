package service

import (
	"context"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/service/allocationoutput"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSealedOutputDistinguishesLostNodeStateFromUnknownObject(t *testing.T) {
	root := t.TempDir()
	service := &sandboxService{config: config.Config{RootDir: root}}

	_, err := service.SealedOutputManifest(context.Background(), "allocation-missing")
	require.Equal(t, codes.Unavailable, status.Code(err))

	const allocationID = "allocation-present"
	require.NoError(t, allocationoutput.NewRetention(root).Seal(
		context.Background(),
		allocationID,
		time.Now().Add(time.Hour),
		allocationoutput.Sources{},
		nil,
	))

	_, _, _, err = service.ReadSealedOutput(
		context.Background(), allocationID, "unknown-output", 0, 1,
	)
	require.Equal(t, codes.NotFound, status.Code(err))
}
