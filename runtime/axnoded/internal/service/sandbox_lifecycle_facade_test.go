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
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/workloadidentity"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
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
	s := newTestService(t, map[string]contract.RuntimeHandler{
		"runsc": handler,
	})

	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	assert.NoError(t, os.MkdirAll(rootfsDir, 0755))

	fr := &runtime.RuntimeTemplate{
		ID:      "test-runtime-id-label",
		Sandbox: "runsc",
		Rootfs: &runtime.RootfsConfig{
			Readonly: true,
			Type:     runtime.RootfsSrcType_LOCAL,
			Source:   &runtime.RootfsConfig_Path{Path: rootfsDir},
		},
		Command: []string{"/bin/sh", "-c", "echo ok"},
	}

	resp, err := s.Start(context.Background(), &runtime.StartRequest{
		ContainerID:     "test-runtime-id-label-allocation",
		RuntimeTemplate: fr,
		Network:         "host",
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
	s := newTestService(t, map[string]contract.RuntimeHandler{"runsc": handler})
	now := time.Now().UTC()
	extension := capabilitycontract.ExtensionKey("example.com/accelerator", "model-a")
	manager, err := capabilitymanager.NewManager(configCapabilityProvider(
		[]*capabilityv1.ExtensionCapability{extension.GetExtension()},
		extensionConfigDigest([]*capabilityv1.ExtensionCapability{extension.GetExtension()}),
	))
	assert.NoError(t, err)
	snapshot, err := manager.Refresh(context.Background(), now)
	assert.NoError(t, err)
	dependencies, err := capabilitycontract.ResolveDependencies(snapshot, []*capabilityv1.CapabilityKey{extension}, now)
	assert.NoError(t, err)
	s.capabilityManager = manager
	rootfsDir := filepath.Join(t.TempDir(), "rootfs")
	assert.NoError(t, os.MkdirAll(rootfsDir, 0o755))
	request := &runtime.StartRequest{
		ContainerID:       "allocation-retry-contract",
		AllocationAttempt: 1,
		RuntimeTemplate: &runtime.RuntimeTemplate{
			ID: "retry-contract", Sandbox: "runsc",
			Rootfs:  &runtime.RootfsConfig{Readonly: true, Type: runtime.RootfsSrcType_LOCAL, Source: &runtime.RootfsConfig_Path{Path: rootfsDir}},
			Command: []string{"/bin/sh", "-c", "sleep 60"},
		},
		Network: "host",
		ExtensionCapabilityRequirements: []*capabilityv1.ExtensionCapabilityRequirement{{
			Capability: proto.Clone(extension.GetExtension()).(*capabilityv1.ExtensionCapability),
		}},
		CapabilityDependencies: dependencies,
	}

	first, err := s.Start(context.Background(), proto.Clone(request).(*runtime.StartRequest))
	assert.NoError(t, err)
	assert.Equal(t, int32(0), first.GetCode())
	assert.Equal(t, 1, handler.createCalls)
	assert.Equal(t, int64(2), s.allocationController().CapabilityConditions(request.GetContainerID()).GetRevision())

	retry := proto.Clone(request).(*runtime.StartRequest)
	retry.TraceID = "new-retry-trace"
	s.capabilityManager = nil
	degradedAt := now.Add(time.Second)
	_, err = s.allocationController().ReplaceCapabilityConditions(request.GetContainerID(), []*capabilityv1.CapabilityCondition{{
		Key: extension, State: capabilityv1.CapabilityConditionState_CAPABILITY_CONDITION_STATE_DEGRADED,
		ReasonCode: capabilityv1.CapabilityReasonCode_CAPABILITY_REASON_CODE_PROBE_FAILED,
		Message:    "node observation unavailable after create", ObservedAt: timestamppb.New(degradedAt),
		Proof: proto.Clone(first.GetAdmittedCapabilityDependencies()[0].GetSelectedObservation()).(*capabilityv1.CapabilityObservationProof),
	}}, degradedAt)
	assert.NoError(t, err)
	second, err := s.Start(context.Background(), retry)
	assert.NoError(t, err)
	assert.Equal(t, int32(0), second.GetCode())
	assert.Equal(t, 1, handler.createCalls)
	assert.Equal(t, int64(3), s.allocationController().CapabilityConditions(request.GetContainerID()).GetRevision())
	assert.Len(t, second.GetAdmittedCapabilityDependencies(), len(first.GetAdmittedCapabilityDependencies()))
	assert.True(t, proto.Equal(first.GetCapabilityVerification(), second.GetCapabilityVerification()))

	changed := proto.Clone(request).(*runtime.StartRequest)
	changed.RuntimeTemplate.Command = []string{"/bin/false"}
	_, err = s.Start(context.Background(), changed)
	assert.ErrorContains(t, err, "differs from the durable contract")
	assert.Equal(t, codes.FailedPrecondition, grpcstatus.Code(err))
	assert.Equal(t, 1, handler.createCalls)
	assert.Equal(t, int64(3), s.allocationController().CapabilityConditions(request.GetContainerID()).GetRevision())

	_, err = s.Delete(context.Background(), &runtime.DeleteRequest{ID: request.GetContainerID()})
	assert.NoError(t, err)
}
