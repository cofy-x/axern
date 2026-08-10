package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	capabilitymanager "github.com/cofy-x/axern/runtime/axnoded/internal/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

func TestRootfsCapabilityGateRebindsCurrentObservationBeforeRuntimeSideEffects(t *testing.T) {
	extension := &capabilityv1.ExtensionCapability{Name: "example.com/accelerator", Value: "model-a"}
	provider := configCapabilityProvider([]*capabilityv1.ExtensionCapability{extension}, extensionConfigDigest([]*capabilityv1.ExtensionCapability{extension}))
	manager, err := capabilitymanager.NewManager(provider)
	if err != nil {
		t.Fatal(err)
	}
	firstAt := time.Now().UTC().Add(-time.Second)
	first, err := manager.Refresh(context.Background(), firstAt)
	if err != nil {
		t.Fatal(err)
	}
	key := capabilitycontract.ExtensionKey(extension.GetName(), extension.GetValue())
	placement, err := capabilitycontract.ResolveDependencies(first, []*capabilityv1.CapabilityKey{key}, firstAt)
	if err != nil {
		t.Fatal(err)
	}
	current, err := manager.Refresh(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	currentObservation, available := capabilitycontract.AvailableObservation(current, key, time.Now().UTC())
	if !available {
		t.Fatal("current extension observation is unavailable")
	}
	rootfs := filepath.Join(t.TempDir(), "rootfs")
	if err := os.Mkdir(rootfs, 0o755); err != nil {
		t.Fatal(err)
	}
	request := &apipb.StartRequest{
		ContainerID: "rootfs-gate-rebind",
		RuntimeTemplate: &apipb.RuntimeTemplate{
			Sandbox: config.RuntimeNameRunsc,
			Rootfs:  &apipb.RootfsConfig{Readonly: true, Type: apipb.RootfsSrcType_LOCAL, Source: &apipb.RootfsConfig_Path{Path: rootfs}},
		},
		Network:                         "host",
		ExtensionCapabilityRequirements: []*capabilityv1.ExtensionCapabilityRequirement{{Capability: extension}},
		CapabilityDependencies:          placement,
	}
	service := &sandboxService{capabilityManager: manager}
	if err := service.verifyRootfsCapabilityRequirements(context.Background(), request, rootfs); err != nil {
		t.Fatal(err)
	}
	got := request.GetCapabilityDependencies()[0].GetSelectedObservation().GetObservationID()
	if got != currentObservation.GetObservationID() || got == placement[0].GetSelectedObservation().GetObservationID() {
		t.Fatalf("rootfs gate proof = %q, current = %q, placement = %q", got, currentObservation.GetObservationID(), placement[0].GetSelectedObservation().GetObservationID())
	}
}

func TestPreActivationGateRebindsCurrentObservationBeforeWorkloadStart(t *testing.T) {
	extension := &capabilityv1.ExtensionCapability{Name: "example.com/accelerator", Value: "model-a"}
	provider := configCapabilityProvider([]*capabilityv1.ExtensionCapability{extension}, extensionConfigDigest([]*capabilityv1.ExtensionCapability{extension}))
	manager, err := capabilitymanager.NewManager(provider)
	if err != nil {
		t.Fatal(err)
	}
	firstAt := time.Now().UTC().Add(-time.Second)
	first, err := manager.Refresh(context.Background(), firstAt)
	if err != nil {
		t.Fatal(err)
	}
	key := capabilitycontract.ExtensionKey(extension.GetName(), extension.GetValue())
	placement, err := capabilitycontract.ResolveDependencies(first, []*capabilityv1.CapabilityKey{key}, firstAt)
	if err != nil {
		t.Fatal(err)
	}
	admitted, conditions, err := manager.AdmitDependencies(placement, firstAt)
	if err != nil {
		t.Fatal(err)
	}

	handler := &runtimeSpyHandler{name: config.RuntimeNameRunsc}
	service := newTestService(t, map[string]contract.RuntimeHandler{config.RuntimeNameRunsc: handler})
	service.capabilityManager = manager
	const allocationID = "pre-activation-rebind"
	const requestDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err := service.allocationController().ReplaceCapabilityAdmission(allocationID, 1, requestDigest, admitted, conditions, firstAt); err != nil {
		t.Fatal(err)
	}
	current, err := manager.Refresh(context.Background(), time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	currentObservation, available := capabilitycontract.AvailableObservation(current, key, time.Now().UTC())
	if !available {
		t.Fatal("current extension observation is unavailable")
	}
	request := &apipb.StartRequest{ContainerID: allocationID, AllocationAttempt: 1, CapabilityDependencies: placement}
	if err := service.verifyPreparedAllocationCapabilities(context.Background(), request, handler, allocationID); err != nil {
		t.Fatal(err)
	}
	got := request.GetCapabilityDependencies()[0].GetSelectedObservation().GetObservationID()
	if got != currentObservation.GetObservationID() || got == placement[0].GetSelectedObservation().GetObservationID() {
		t.Fatalf("pre-activation proof = %q, current = %q, placement = %q", got, currentObservation.GetObservationID(), placement[0].GetSelectedObservation().GetObservationID())
	}
}

func TestPrepareNodeLocalStartRequestBindsCurrentExactProofs(t *testing.T) {
	now := time.Now().UTC()
	port := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)
	bpfnet := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_NETWORK_BPFNET)
	overlay := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_FILESTORE_OVERLAYFS_UPPER)
	selfTest := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST)
	hardLimit := capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT)
	configEvidence := capabilitycontract.ConfigEvidence("sha256:" + strings.Repeat("a", 64))
	network := observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_NETWORK_HEALTH,
		expected: []*capabilityv1.CapabilityKey{port, bpfnet},
		observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{availableObservation(port, configEvidence), availableObservation(bpfnet, configEvidence)}, nil
		},
	}
	filestore := observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_FILESTORE,
		expected: []*capabilityv1.CapabilityKey{overlay},
		observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			return []*capabilityv1.CapabilityObservation{availableObservation(overlay, capabilitycontract.MountEvidence(testCapabilityBootID, "42:/dev/loop0:/filestore"))}, nil
		},
	}
	runtimeSelfTest := observedProvider{
		provider: capabilityv1.CapabilityProvider_CAPABILITY_PROVIDER_RUNSC_SELF_TEST,
		expected: []*capabilityv1.CapabilityKey{selfTest},
		observe: func(context.Context, time.Time) ([]*capabilityv1.CapabilityObservation, error) {
			evidence := capabilitycontract.RuntimeEvidence(testCapabilityBootID, config.RuntimeNameRunsc, sha256Digest([]byte("runsc")), sha256Digest([]byte("config")))
			return []*capabilityv1.CapabilityObservation{availableObservation(selfTest, evidence)}, nil
		},
	}
	manager, err := capabilitymanager.NewManager(network, filestore, runtimeSelfTest, derivedCapabilityProvider{expected: []*capabilityv1.CapabilityKey{hardLimit}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Refresh(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	service := &sandboxService{
		config:            config.Config{PluginConfig: config.PluginConfig{NetworkConfig: config.NetworkConfig{NatBackend: config.NatBackendEBPF}}},
		capabilityManager: manager,
	}
	request := &apipb.StartRequest{
		ContainerID: "node-local-test",
		RuntimeTemplate: &apipb.RuntimeTemplate{
			Sandbox: config.RuntimeNameRunsc,
			Rootfs:  &apipb.RootfsConfig{Type: apipb.RootfsSrcType_LOCAL, Source: &apipb.RootfsConfig_Path{Path: t.TempDir()}},
		},
		Ports: []string{"tcp:18080:80"},
	}
	prepared, err := service.prepareNodeLocalStartRequest(request, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(request.GetCapabilityDependencies()) != 0 {
		t.Fatal("node-local request was mutated in place")
	}
	keys, err := dependencyKeys(prepared.GetCapabilityDependencies(), false)
	if err != nil {
		t.Fatal(err)
	}
	want := []*capabilityv1.CapabilityKey{port, bpfnet, hardLimit}
	if !capabilitycontract.RequirementKeysEqual(keys, want) {
		t.Fatalf("derived keys = %#v, want %#v", keys, want)
	}
	for _, dependency := range prepared.GetCapabilityDependencies() {
		if dependency.GetSelectedSnapshot().GetSnapshotID() == "" || dependency.GetSelectedObservation().GetObservationID() == "" {
			t.Fatalf("dependency is missing current snapshot proof: %#v", dependency)
		}
	}
}

func TestPrepareNodeLocalStartRequestRejectsInjectedOrUnobservedProofs(t *testing.T) {
	service := &sandboxService{}
	request := &apipb.StartRequest{
		RuntimeTemplate: &apipb.RuntimeTemplate{
			Sandbox: config.RuntimeNameRunsc,
			Rootfs:  &apipb.RootfsConfig{Type: apipb.RootfsSrcType_LOCAL, Source: &apipb.RootfsConfig_Path{Path: t.TempDir()}},
		},
		CapabilityDependencies: []*capabilityv1.CapabilityDependency{{Key: capabilitycontract.PlatformKey(capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_PORT_FORWARDING)}},
	}
	if _, err := service.prepareNodeLocalStartRequest(request, time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "cannot supply") {
		t.Fatalf("injected dependency error = %v", err)
	}
	request.CapabilityDependencies = nil
	if _, err := service.prepareNodeLocalStartRequest(request, time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "warming") {
		t.Fatalf("warming manager error = %v", err)
	}
	request.AllocationAttempt = 1
	if _, err := service.prepareNodeLocalStartRequest(request, time.Now().UTC()); err == nil || !strings.Contains(err.Error(), "control-plane") {
		t.Fatalf("control-plane attempt error = %v", err)
	}
}
