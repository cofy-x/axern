package runtime

import (
	"context"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/ocihost"
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/stretchr/testify/assert"
)

func TestRuncHandlerKillContainerUsesOCIKill(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRuncServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunc, config.RuntimeInstanceConfig{Binary: "/usr/bin/runc"}, loader)
	if err != nil {
		t.Fatalf("NewRuncServiceHandler() error = %v", err)
	}
	recorder := &recordingExecutor{}
	handler.common.SetExecutor(recorder)

	_, err = handler.KillContainer(context.Background(), &apipb.SignalContainerRequest{
		ID:     "axctl-test",
		Signal: "TERM",
	}, contract.HandlerOptions{ContainerID: "axctl-test"})
	assert.NoError(t, err)
	if assert.Len(t, recorder.args, 1) {
		assert.Equal(t, []string{"--root", filepath.Join(rootDir, config.RuntimeNameRunc), "kill", "axctl-test", "TERM"}, recorder.args[0])
	}
}

func TestRuncPrepareContainerUsesCreate(t *testing.T) {
	rootDir := t.TempDir()
	writeFakeSandboxdBinary(t, rootDir)
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRuncServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunc, config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runc"}, loader)
	if err != nil {
		t.Fatalf("NewRuncServiceHandler() error = %v", err)
	}
	handler.ignoreCgroups = true
	recorder := &recordingExecutor{}
	handler.common.SetExecutor(recorder)
	var monitorOptions ocihost.InitMonitorStartOptions
	handler.common.SetInitMonitorStarter(func(_ context.Context, options ocihost.InitMonitorStartOptions) error {
		monitorOptions = options
		return os.WriteFile(options.RuntimePIDPath, []byte("1234\n"), 0644)
	})

	request := newLocalCreateRequest(t)
	request.Runtime = "runc"
	request.Command = []string{"/bin/sh"}
	prepared, err := handler.PrepareContainer(context.Background(), request, contract.HandlerOptions{ContainerID: "allocation-prepared"})
	assert.NoError(t, err)
	if assert.NotNil(t, prepared) {
		assert.Equal(t, "allocation-prepared", prepared.ContainerID)
		assert.NotEmpty(t, prepared.BundlePath)
	}
	assert.Contains(t, monitorOptions.RuntimeArgs, "create")
	assert.NotContains(t, monitorOptions.RuntimeArgs, "run")
	assert.Equal(t, prepared.Metadata.GetStdout(), monitorOptions.StdoutPath)
	assert.Equal(t, prepared.Metadata.GetStderr(), monitorOptions.StderrPath)
	assert.Equal(t, handler.common.RuntimePIDFilePath("allocation-prepared"), monitorOptions.RuntimePIDPath)
}

func TestRuncStartPreparedContainerUsesStart(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRuncServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunc, config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runc"}, loader)
	if err != nil {
		t.Fatalf("NewRuncServiceHandler() error = %v", err)
	}
	disableSandboxReadyWait(t, handler)
	recorder := &recordingExecutor{}
	handler.common.SetExecutor(recorder)
	pidPath := handler.common.RuntimePIDFilePath("allocation-prepared")
	assert.NoError(t, os.MkdirAll(filepath.Dir(pidPath), 0755))

	assert.NoError(t, os.WriteFile(pidPath, []byte("1234\n"), 0644))

	meta, err := handler.StartPreparedContainer(context.Background(), &contract.PreparedContainer{
		ContainerID: "allocation-prepared",
		Metadata: &apipb.ContainerMetadata{
			ID:             "allocation-prepared",
			RuntimeHandler: "runc",
		},
	}, contract.HandlerOptions{ContainerID: "allocation-prepared"})
	assert.NoError(t, err)
	if assert.NotNil(t, meta) {
		assert.Equal(t, "allocation-prepared", meta.ID)
	}

	args := recorder.Args()
	if assert.NotEmpty(t, args) {
		assert.Contains(t, args[0], "start")
	}
	for _, entry := range args {
		assert.NotContains(t, entry, "wait", "runc has no portable wait command; the create-time init monitor owns exit collection")
	}
}
