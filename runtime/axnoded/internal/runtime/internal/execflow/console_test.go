package execflow

import (
	"os"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestWriteConsoleRetriesEAGAIN(t *testing.T) {
	console, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open devnull: %v", err)
	}
	defer console.Close()

	var (
		writeCalls int
		waitCalls  int
		written    []byte
	)
	writeChunk := func(_ *os.File, data []byte) (int, error) {
		writeCalls++
		switch writeCalls {
		case 1:
			return 0, syscall.EAGAIN
		default:
			written = append(written, data...)
			return len(data), nil
		}
	}
	waitWritable := func(_ *os.File) error {
		waitCalls++
		return nil
	}

	err = WriteConsole(console, []byte("hello"), writeChunk, waitWritable)
	assert.NoError(t, err)
	assert.Equal(t, 2, writeCalls)
	assert.Equal(t, 1, waitCalls)
	assert.Equal(t, []byte("hello"), written)
}

func TestReadOutputFileLimitedMarksTruncation(t *testing.T) {
	path := t.TempDir() + "/stdout.log"
	large := make([]byte, MaxExecOutputBytes+128)
	for i := range large {
		large[i] = 'a'
	}
	assert.NoError(t, os.WriteFile(path, large, 0600))

	data, truncated, err := ReadOutputFileLimited(path)
	assert.NoError(t, err)
	assert.True(t, truncated)
	assert.Len(t, data, MaxExecOutputBytes)
}
