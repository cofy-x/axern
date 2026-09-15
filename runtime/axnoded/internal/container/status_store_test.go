package container

import (
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/stretchr/testify/assert"
)

func TestUpdateSync(t *testing.T) {
	const NewPid = 456
	const SuccessKey = "2026-09-13T12:00:01Z"

	containerRoot := t.TempDir()
	ss := statusStorage{
		path: filepath.Join(containerRoot, config.ContainerStatusFile),
		status: Status{
			RuntimeState: apipb.RuntimeCheckpointState_RUNTIME_CHECKPOINT_STATE_RUNNING,
			Pid:          123,
			StartedAt:    "2026-09-13T12:00:00Z",
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

func TestLoadStatusDoesNotInferLifecycleFromRuntimePIDFile(t *testing.T) {
	containerRoot := t.TempDir()

	status, err := LoadStatus(containerRoot)
	assert.NoError(t, err)
	assert.Equal(t, apipb.ContainerState_CONTAINER_UNKNOWN, status.Get().State())
	assert.Equal(t, 0, status.Get().Pid)
}
