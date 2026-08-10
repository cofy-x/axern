package verifyutil

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	capabilitycontract "github.com/cofy-x/axern/lib/go/nodecapability"
	"github.com/cofy-x/axern/runtime/axnoded/internal/nodeinventory"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/rootfsview"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	privatenodev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/node/lifecycle/v1"
	"google.golang.org/protobuf/proto"
)

const maxInventoryResponseBytes = 8 << 20

var errNodeInventoryWarming = errors.New("node capability inventory is warming")

// prepareCapabilityDependencies makes the repository verification clients act
// like controld: they select a single published node snapshot, derive typed
// requirements with the shared catalog, and bind every requirement to its
// proof. The production lifecycle gate remains fail-closed and has no
// verification-only RPC bypass.
func prepareCapabilityDependencies(ctx context.Context, clients *NodeClients, spec *privatenodev1.ResolvedExecutionConfig) (*privatenodev1.ResolvedExecutionConfig, error) {
	if clients == nil || clients.httpClient == nil || strings.TrimSpace(clients.inventoryURL) == "" {
		return nil, fmt.Errorf("node inventory client is unavailable")
	}
	if spec == nil {
		return nil, fmt.Errorf("resolved execution config is required")
	}
	prepared := proto.Clone(spec).(*privatenodev1.ResolvedExecutionConfig)
	if len(prepared.GetCapabilityDependencies()) != 0 {
		return prepared, nil
	}

	inventory, err := waitForNodeInventory(ctx, clients.httpClient, clients.inventoryURL)
	if err != nil {
		return nil, err
	}
	snapshot := inventory.Node.CapabilitySnapshot
	now := time.Now().UTC()
	backend := ""
	if networkMode(prepared.GetNetwork()) != "host" {
		backend = capabilitycontract.AvailableNetworkBackend(snapshot, now)
		if backend == "" {
			return nil, fmt.Errorf("node has no currently available network backend")
		}
	}
	erofs, err := verificationEROFSBacking(prepared, inventory)
	if err != nil {
		return nil, err
	}
	resources := prepared.GetResources()
	requirements, err := capabilitycontract.DeriveRequirements(capabilitycontract.RequirementInput{
		RuntimeName:                 prepared.GetRuntimeClass(),
		HasPorts:                    len(prepared.GetPorts()) > 0,
		NetworkMode:                 networkMode(prepared.GetNetwork()),
		NetworkBackend:              backend,
		MemoryLimitBytes:            resources.GetLimits().GetMemoryBytes(),
		RootfsWritable:              !prepared.GetRootfsReadonly(),
		EphemeralStorageLimitBytes:  resources.GetLimits().GetEphemeralStorageBytes(),
		EROFSBacking:                erofs,
		ExtensionCapabilityRequests: prepared.GetExtensionCapabilityRequirements(),
	})
	if err != nil {
		return nil, fmt.Errorf("derive requirements: %w", err)
	}
	dependencies, err := capabilitycontract.ResolveDependencies(snapshot, requirements, now)
	if err != nil {
		return nil, fmt.Errorf("resolve requirements from node snapshot: %w", err)
	}
	prepared.CapabilityDependencies = dependencies
	return prepared, nil
}

func waitForNodeInventory(ctx context.Context, client *http.Client, inventoryURL string) (nodeinventory.NodeInventorySnapshot, error) {
	const retryInterval = 250 * time.Millisecond
	for {
		snapshot, err := fetchNodeInventory(ctx, client, inventoryURL)
		if err == nil {
			return snapshot, nil
		}
		if !errors.Is(err, errNodeInventoryWarming) {
			return nodeinventory.NodeInventorySnapshot{}, err
		}
		timer := time.NewTimer(retryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nodeinventory.NodeInventorySnapshot{}, fmt.Errorf("wait for node capability inventory: %w", ctx.Err())
		case <-timer.C:
		}
	}
}

func fetchNodeInventory(ctx context.Context, client *http.Client, inventoryURL string) (nodeinventory.NodeInventorySnapshot, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, inventoryURL, nil)
	if err != nil {
		return nodeinventory.NodeInventorySnapshot{}, fmt.Errorf("build node inventory request: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return nodeinventory.NodeInventorySnapshot{}, fmt.Errorf("fetch node inventory: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64<<10))
		if response.StatusCode == http.StatusServiceUnavailable {
			return nodeinventory.NodeInventorySnapshot{}, fmt.Errorf("%w: %s", errNodeInventoryWarming, response.Status)
		}
		return nodeinventory.NodeInventorySnapshot{}, fmt.Errorf("fetch node inventory: unexpected HTTP status %s", response.Status)
	}
	var snapshot nodeinventory.NodeInventorySnapshot
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxInventoryResponseBytes+1))
	if err := decoder.Decode(&snapshot); err != nil {
		return nodeinventory.NodeInventorySnapshot{}, fmt.Errorf("decode node inventory: %w", err)
	}
	if snapshot.Node.CapabilitySnapshot == nil {
		return nodeinventory.NodeInventorySnapshot{}, fmt.Errorf("node inventory has no capability snapshot")
	}
	return snapshot, nil
}

func verificationEROFSBacking(spec *privatenodev1.ResolvedExecutionConfig, inventory nodeinventory.NodeInventorySnapshot) (bool, error) {
	if rootfsPath := strings.TrimSpace(spec.GetLocalRootfsPath()); rootfsPath != "" {
		facts, err := rootfsview.InspectBacking(rootfsPath)
		if err != nil {
			return false, fmt.Errorf("inspect local rootfs backing: %w", err)
		}
		if strings.EqualFold(facts.FSType, "erofs") {
			return true, nil
		}
	}
	localityKey := strings.TrimSpace(spec.GetLocalityKey())
	for _, locality := range inventory.Heat.Locality {
		if locality.Key == localityKey && strings.EqualFold(locality.MountType, "erofs") {
			return true, nil
		}
	}
	return false, nil
}

func networkMode(spec *commonv1.NetworkSpec) string {
	if spec == nil || spec.GetMode() == commonv1.NetworkMode_NETWORK_MODE_UNSPECIFIED {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(spec.GetMode().String(), "NETWORK_MODE_"))
}
