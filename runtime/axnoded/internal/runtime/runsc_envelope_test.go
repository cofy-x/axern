package runtime

import (
	"context"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"github.com/stretchr/testify/assert"
)

func TestRunscHandlerKillContainerUsesOCIKill(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunsc, config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"}, loader)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}
	recorder := &recordingExecutor{}
	handler.common.SetExecutor(recorder)

	_, err = handler.KillContainer(context.Background(), &apipb.SignalContainerRequest{
		ID:     "axctl-test",
		Signal: "TERM",
	}, contract.HandlerOptions{ContainerID: "axctl-test"})
	assert.NoError(t, err)
	if assert.Len(t, recorder.args, 1) {
		assert.Equal(t, []string{"--root", filepath.Join(rootDir, config.RuntimeNameRunsc), "--allow-suid", "kill", "axctl-test", "TERM"}, recorder.args[0])
	}
}

func TestRunscPrepareExecutionEnvelopeUsesCreate(t *testing.T) {
	rootDir := t.TempDir()
	writeFakeSandboxdBinary(t, rootDir)
	rootfsDir := newReadonlyRootfs(t)
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunsc, config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"}, loader)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}
	recorder := &recordingExecutor{}
	handler.common.SetExecutor(recorder)

	envelope, err := handler.PrepareExecutionEnvelope(context.Background(), &apipb.CreateContainerRequest{
		Runtime: "runsc",
		Command: []string{"/bin/sh"},
		Rootfs: &apipb.Rootfs{
			RootDir:  rootfsDir,
			Readonly: true,
		},
	}, contract.HandlerOptions{ContainerID: "axctl-prewarm"})
	assert.NoError(t, err)
	if assert.NotNil(t, envelope) {
		assert.Equal(t, "axctl-prewarm", envelope.ContainerID)
		assert.NotEmpty(t, envelope.BundlePath)
	}
	args := recorder.Args()
	if assert.Len(t, args, 1) {
		assert.Contains(t, args[0], "create")
		assert.NotContains(t, args[0], "run")
	}
}

func TestRunscActivateExecutionEnvelopeUsesStart(t *testing.T) {
	rootDir := t.TempDir()
	loader, err := runtimeoci.NewBundleLoader("", filepath.Join(rootDir, "containers"))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	handler, err := NewRunscServiceHandler(config.Config{RootDir: rootDir}, config.RuntimeNameRunsc, config.RuntimeInstanceConfig{Binary: "/usr/local/bin/runsc"}, loader)
	if err != nil {
		t.Fatalf("NewRunscServiceHandler() error = %v", err)
	}
	disableSandboxReadyWait(t, handler)
	recorder := &recordingExecutor{}
	handler.common.SetExecutor(recorder)
	pidPath := handler.common.RuntimePIDFilePath("axctl-prewarm")
	assert.NoError(t, os.MkdirAll(filepath.Dir(pidPath), 0755))

	assert.NoError(t, os.WriteFile(pidPath, []byte("1234\n"), 0644))

	meta, err := handler.ActivateExecutionEnvelope(context.Background(), &contract.ExecutionEnvelope{
		ContainerID: "axctl-prewarm",
		Metadata: &apipb.ContainerMetadata{
			ID:             "axctl-prewarm",
			RuntimeHandler: "runsc",
		},
	}, contract.HandlerOptions{ContainerID: "axctl-prewarm"})
	assert.NoError(t, err)
	if assert.NotNil(t, meta) {
		assert.Equal(t, "axctl-prewarm", meta.ID)
	}

	deadline := time.Now().Add(time.Second)
	var args [][]string
	for time.Now().Before(deadline) {
		args = recorder.Args()
		if len(args) >= 2 {
			foundWait := false
			for _, entry := range args {
				if containsArg(entry, "wait") {
					foundWait = true
					break
				}
			}
			if foundWait {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if assert.GreaterOrEqual(t, len(args), 2) {
		assert.Contains(t, args[0], "start")
		foundWait := false
		for _, entry := range args {
			if containsArg(entry, "wait") {
				foundWait = true
				break
			}
		}
		assert.True(t, foundWait)
	}
	waitForPersistedExitState(t, func() (contract.Exit, bool, error) {
		return handler.readExitState("axctl-prewarm")
	})
}
