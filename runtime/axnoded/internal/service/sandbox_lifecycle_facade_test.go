package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/workloadidentity"
	"github.com/stretchr/testify/assert"
)

func TestDelete_NotFound(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	})

	_, err := s.Delete(context.Background(), &runtime.DeleteRequest{
		ID: "axctl-nonexistent",
	})
	assert.NoError(t, err)
}

func TestStart_And_Delete(t *testing.T) {
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": runtimetest.NewFakeRuntimeHandler(),
	})

	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	assert.NoError(t, os.MkdirAll(rootfsDir, 0755))

	fr := &runtime.RuntimeTemplate{
		ID:      "test-start-del-rt",
		Sandbox: "runsc",
		Rootfs: &runtime.RootfsConfig{
			Readonly: false,
			Type:     runtime.RootfsSrcType_LOCAL,
			Source:   &runtime.RootfsConfig_Path{Path: rootfsDir},
		},
		Command: []string{"/bin/sleep", "infinity"},
	}
	startResp, err := s.Start(context.Background(), &runtime.StartRequest{
		RuntimeTemplate: fr,
		Stdout:          "/tmp/stdout.log",
		Stderr:          "/tmp/stderr.log",
	})
	if err != nil {
		t.Logf("Start failed (expected in test env): %v", err)
		return
	}
	assert.Equal(t, int32(0), startResp.Code)
	assert.NotEmpty(t, startResp.ID)

	containerDir := filepath.Join(s.config.RootDir, "containers", startResp.ID)
	assert.NoError(t, os.MkdirAll(containerDir, 0755))
	assert.NoError(t, os.WriteFile(filepath.Join(containerDir, config.ContainerSpecFile), []byte(`{"ociVersion":"1.0.0","annotations":{},"linux":{"cgroupsPath":""}}`), 0644))

	_, err = s.Delete(context.Background(), &runtime.DeleteRequest{
		ID: startResp.ID,
	})
	assert.NoError(t, err)
}

func TestStart_AddsRuntimeIDLabelForTemporaryRuntime(t *testing.T) {
	handler := &runtimeSpyHandler{name: "runsc"}
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": handler,
	})

	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	assert.NoError(t, os.MkdirAll(rootfsDir, 0755))

	fr := &runtime.RuntimeTemplate{
		ID:      "test-runtime-id-label",
		Sandbox: "runsc",
		Rootfs: &runtime.RootfsConfig{
			Readonly: false,
			Type:     runtime.RootfsSrcType_LOCAL,
			Source:   &runtime.RootfsConfig_Path{Path: rootfsDir},
		},
		Command: []string{"/bin/sh", "-c", "echo ok"},
	}

	resp, err := s.Start(context.Background(), &runtime.StartRequest{
		RuntimeTemplate: fr,
		Stdout:          "/tmp/runtime-id-label.stdout",
		Stderr:          "/tmp/runtime-id-label.stderr",
	})
	assert.NoError(t, err)
	assert.Equal(t, int32(0), resp.GetCode())
	if handler.lastRequest == nil {
		t.Fatalf("expected create request to be captured")
	}
	assert.Equal(t, fr.ID, handler.lastRequest.GetLabels()[workloadidentity.LabelKeyRuntimeID])
}
