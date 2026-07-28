package execflow

import (
	"errors"
	"io"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionStateRecvWaitAndClose(t *testing.T) {
	closeErr := errors.New("close failed")
	session := NewSessionState()
	session.SetCloseFunc(func() error { return closeErr })

	session.EmitStdout([]byte("out"))
	session.EmitStderr([]byte("err"))
	session.FinishWait(contract.Exit{Timestamp: time.Unix(1, 0), Status: 7}, nil)
	session.FinishOutput()

	chunk, err := session.Recv()
	require.NoError(t, err)
	assert.Equal(t, []byte("out"), chunk.Stdout)

	chunk, err = session.Recv()
	require.NoError(t, err)
	assert.Equal(t, []byte("err"), chunk.Stderr)

	_, err = session.Recv()
	assert.ErrorIs(t, err, io.EOF)

	exit, err := session.Wait()
	require.NoError(t, err)
	assert.Equal(t, 7, exit.Status)
	assert.Equal(t, time.Unix(1, 0), exit.Timestamp)

	assert.ErrorIs(t, session.Close(), closeErr)
	assert.ErrorIs(t, session.Signal("TERM"), closeErr)
}

func TestSessionStateCopiesOutputChunks(t *testing.T) {
	session := NewSessionState()
	stdout := []byte("out")
	stderr := []byte("err")

	session.EmitStdout(stdout)
	session.EmitStderr(stderr)
	stdout[0] = 'x'
	stderr[0] = 'x'
	session.FinishOutput()

	chunk, err := session.Recv()
	require.NoError(t, err)
	assert.Equal(t, []byte("out"), chunk.Stdout)

	chunk, err = session.Recv()
	require.NoError(t, err)
	assert.Equal(t, []byte("err"), chunk.Stderr)
}
