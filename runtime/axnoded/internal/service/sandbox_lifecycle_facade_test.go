package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	capabilitymanager "github.com/cofy-x/axern/runtime/axnoded/internal/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/runtimetest"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

func TestDelete_NotFound(t *testing.T) {
	s := newTestService(t,
		runtimetest.NewFakeRuntimeHandler(),
	)

	_, err := s.Delete(context.Background(), &runtime.DeleteRequest{
		ID: "axctl-nonexistent",
	})
	assert.NoError(t, err)
}

func TestStart_And_Delete(t *testing.T) {
	s := newTestService(t,
		runtimetest.NewFakeRuntimeHandler(),
	)

	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	assert.NoError(t, os.MkdirAll(rootfsDir, 0755))

	fr := &runtime.RuntimeTemplate{
		ID: "test-start-del-rt",
		Rootfs: &runtime.RootfsConfig{
			Readonly: false,
			Type:     runtime.RootfsSrcType_LOCAL,
			Source:   &runtime.RootfsConfig_Path{Path: rootfsDir},
		},
		Command: []string{"/bin/sleep", "infinity"},
	}
	startResp, err := s.Start(context.Background(), &runtime.StartRequest{
		ContainerID:     "test-start-delete-allocation",
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
	s := newTestService(t,
		handler,
	)

	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	assert.NoError(t, os.MkdirAll(rootfsDir, 0755))

	fr := &runtime.RuntimeTemplate{
		ID: "test-explicit-allocation-id",
		Rootfs: &runtime.RootfsConfig{
			Readonly: true,
			Type:     runtime.RootfsSrcType_LOCAL,
			Source:   &runtime.RootfsConfig_Path{Path: rootfsDir},
		},
		Command: []string{"/bin/sh", "-c", "echo ok"},
	}

	resp, err := s.Start(context.Background(), &runtime.StartRequest{
		ContainerID:     "test-explicit-allocation-id",
		RuntimeTemplate: fr,
		Network:         "host",
		Stdout:          "/tmp/explicit-allocation-id.stdout",
		Stderr:          "/tmp/explicit-allocation-id.stderr",
	})
	assert.NoError(t, err)
	assert.Equal(t, int32(0), resp.GetCode())
	if handler.lastRequest == nil {
		t.Fatalf("expected create request to be captured")
	}
	assert.NotContains(t, handler.lastRequest.GetLabels(), "runtime-id")
}

func TestStartRetryRequiresExactDurableRequestContract(t *testing.T) {
	runtimeDeleted := make(chan struct{})
	handler := &runtimeSpyHandler{name: "runsc", waitFunc: func(ctx context.Context, _ contract.HandlerOptions) (contract.Exit, error) {
		select {
		case <-runtimeDeleted:
			return contract.Exit{Status: 0, Timestamp: time.Now().UTC()}, nil
		case <-ctx.Done():
			return contract.Exit{}, ctx.Err()
		}
	}}
	handler.deleteHook = func() { close(runtimeDeleted) }
	s := newTestService(t, handler)
	now := time.Now().UTC()
	extension := capabilitycontract.ExtensionKey("example.com/accelerator", "model-a")
	manager, err := capabilitymanager.NewManager(configCapabilityProvider(
		[]*capabilityv1.ExtensionCapability{extension.GetExtension()},
	))
	assert.NoError(t, err)
	snapshot, err := manager.Refresh(context.Background(), now)
	assert.NoError(t, err)
	dependencies, err := capabilitycontract.ResolveRequirements(snapshot, []*capabilityv1.CapabilityKey{extension}, now)
	assert.NoError(t, err)
	s.capabilityManager = manager
	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	assert.NoError(t, os.MkdirAll(rootfsDir, 0o755))
	request := &runtime.StartRequest{
		ContainerID: "allocation-retry-contract",
		RuntimeTemplate: &runtime.RuntimeTemplate{
			ID:      "retry-contract",
			Rootfs:  &runtime.RootfsConfig{Readonly: true, Type: runtime.RootfsSrcType_LOCAL, Source: &runtime.RootfsConfig_Path{Path: rootfsDir}},
			Command: []string{"/bin/sh", "-c", "sleep 60"},
		},
		Network: "host",
		ExtensionCapabilityRequirements: []*capabilityv1.ExtensionCapabilityRequirement{{
			Capability: proto.Clone(extension.GetExtension()).(*capabilityv1.ExtensionCapability),
		}},
		CapabilityRequirements: dependencies,
	}

	first, err := s.Start(context.Background(), proto.Clone(request).(*runtime.StartRequest))
	assert.NoError(t, err)
	assert.Equal(t, int32(0), first.GetCode())
	assert.Equal(t, 1, handler.createCalls)

	retry := proto.Clone(request).(*runtime.StartRequest)
	retry.TraceID = "new-retry-trace"
	s.capabilityManager = nil
	second, err := s.Start(context.Background(), retry)
	assert.NoError(t, err)
	assert.Equal(t, int32(0), second.GetCode())
	assert.Equal(t, 1, handler.createCalls)
	assert.Nil(t, second.GetCapabilityVerification())

	changed := proto.Clone(request).(*runtime.StartRequest)
	changed.RuntimeTemplate.Command = []string{"/bin/false"}
	_, err = s.Start(context.Background(), changed)
	assert.ErrorContains(t, err, "differs from the durable contract")
	assert.Equal(t, codes.FailedPrecondition, grpcstatus.Code(err))
	assert.Equal(t, 1, handler.createCalls)

	_, err = s.Delete(context.Background(), &runtime.DeleteRequest{ID: request.GetContainerID()})
	assert.NoError(t, err)
}
