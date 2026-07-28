package imagefsd

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

func newTestManager(daemons map[string]*Daemon) *manager {
	return &manager{
		ctx:     context.Background(),
		daemons: daemons,
	}
}

func newTestDaemon(id string) *Daemon {
	d := &Daemon{
		ctx: context.Background(),
		meta: DaemonMeta{
			ID:   id,
			Name: id,
		},
		config: &BackendConfig{},
	}
	d.setState(DaemonStateStopped)
	d.kickStop = NewStopper()
	return d
}

type mockDaemon struct {
	*Daemon
	mockIsAlive    func() bool
	mockStartMount func() error
	mockStopMount  func() error
}

func (m *mockDaemon) IsAlive() bool {
	if m.mockIsAlive != nil {
		return m.mockIsAlive()
	}
	pidData, err := os.ReadFile(m.meta.PidFilePath)
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = process.Signal(syscall.Signal(0))
	return err == nil
}

func newMockDaemon(tmpDir string) *mockDaemon {
	d := &Daemon{
		ctx: context.Background(),
		meta: DaemonMeta{
			ID:            "test-daemon",
			Name:          "test",
			MountPoint:    filepath.Join(tmpDir, "mnt"),
			DaemonDir:     filepath.Join(tmpDir, "daemon"),
			DaemonLogPath: filepath.Join(tmpDir, "daemon.log"),
			PidFilePath:   filepath.Join(tmpDir, "daemon.pid"),
			CfgPath:       filepath.Join(tmpDir, "config.json"),
			CachePath:     filepath.Join(tmpDir, "cache"),
			ImageMetaDir:  filepath.Join(tmpDir, "meta"),
			ChunkDBDir:    filepath.Join(tmpDir, "chunkdb"),
			SourceType:    "oss",
		},
		savedPath: filepath.Join(tmpDir, "daemon.json"),
		binPath:   "/usr/bin/imagefsd",
		config:    &BackendConfig{},
	}
	d.setState(DaemonStateStopped)
	d.kickStop = NewStopper()

	mock := &mockDaemon{Daemon: d}
	d.isAliveFunc = func() bool {
		return mock.IsAlive()
	}

	return mock
}
