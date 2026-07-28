package container

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/stretchr/testify/assert"
)

func TestUpdateSync(t *testing.T) {
	const NewPid = 456
	const SuccessKey = "success"

	containerRoot := t.TempDir()
	ss := statusStorage{
		path: filepath.Join(containerRoot, config.ContainerStatusFile),
		status: Status{
			Pid:       123,
			StartedAt: "202308201132",
		},
	}

	// No change no update
	err := ss.UpdateSync(func(s Status) (Status, error) {
		return s, nil
	})
	assert.NoError(t, err)

	// Has changes, got status file
	err = ss.UpdateSync(func(s Status) (Status, error) {
		s.Pid = NewPid
		// Mocked WriteFile needs this key so we can get the success result
		s.FinishedAt = SuccessKey
		return s, nil
	})

	assert.NoError(t, err)
	assert.Equal(t, ss.status.Pid, NewPid)
	assert.Equal(t, ss.status.FinishedAt, SuccessKey)
}

func TestLoadStatusHydratesPIDFromRuntimeFile(t *testing.T) {
	containerRoot := t.TempDir()
	assert.NoError(t, os.WriteFile(filepath.Join(containerRoot, "runtime.pid"), []byte("196\n"), 0644))

	status, err := LoadStatus(containerRoot)
	assert.NoError(t, err)
	assert.Equal(t, 196, status.Get().Pid)
}
