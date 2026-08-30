package localruntime

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
)

func TestLocalNodeReadinessPayloadRequiresDefaultWorkloadCapabilities(t *testing.T) {
	validUntil := time.Now().Add(time.Minute)
	payload := readyLocalNodePayload(validUntil)
	if ready, reason := evaluateLocalNodeReadiness(payload, LocalNodeID, localDefaultWorkloadCapabilities, time.Now()); !ready {
		t.Fatalf("ready payload rejected: %s", reason)
	}

	payload.Nodes[0].Summary.CapabilitySnapshot.Observations[1].State = capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE
	payload.Nodes[0].Summary.CapabilitySnapshot.Observations[1].Reason = "quota probe failed"
	if ready, reason := evaluateLocalNodeReadiness(payload, LocalNodeID, localDefaultWorkloadCapabilities, time.Now()); ready || reason != "required local capability PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT is warming or unavailable: quota probe failed" {
		t.Fatalf("unavailable capability result = ready:%t reason:%q", ready, reason)
	}
}

func TestLocalNodeReadinessReportsUnavailableDependency(t *testing.T) {
	payload := readyLocalNodePayload(time.Now().Add(time.Minute))
	derived := &payload.Nodes[0].Summary.CapabilitySnapshot.Observations[1]
	derived.State = capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE
	derived.Reason = "one or more capability dependencies are unavailable"
	var dependency struct {
		Key struct {
			Kind struct {
				Platform capabilityv1.PlatformCapability `json:"Platform"`
			} `json:"Kind"`
		} `json:"key"`
	}
	dependency.Key.Kind.Platform = capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST
	derived.Dependencies = append(derived.Dependencies, dependency)
	base := localCapabilityObservation{State: capabilityv1.CapabilityState_CAPABILITY_STATE_UNAVAILABLE, Reason: "sandbox exited before quota result"}
	base.Key.Kind.Platform = dependency.Key.Kind.Platform
	payload.Nodes[0].Summary.CapabilitySnapshot.Observations = append(payload.Nodes[0].Summary.CapabilitySnapshot.Observations, base)

	ready, reason := evaluateLocalNodeReadiness(payload, LocalNodeID, localDefaultWorkloadCapabilities, time.Now())
	if ready || reason != "required local capability PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_STORAGE_HARD_LIMIT is warming or unavailable: dependency PLATFORM_CAPABILITY_RUNSC_EPHEMERAL_ENFORCEMENT_SELF_TEST: sandbox exited before quota result" {
		t.Fatalf("dependency result = ready:%t reason:%q", ready, reason)
	}
}

func TestLocalNodeReadinessPayloadRejectsExpiredCapability(t *testing.T) {
	payload := readyLocalNodePayload(time.Now().Add(-time.Second))
	if ready, reason := evaluateLocalNodeReadiness(payload, LocalNodeID, localDefaultWorkloadCapabilities, time.Now()); ready || reason == "" {
		t.Fatalf("expired capability result = ready:%t reason:%q", ready, reason)
	}
}

func TestLocalNodeReadinessPayloadRejectsIncompleteNodeState(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*localNodeReadinessPayload)
	}{
		{name: "stale summary", mutate: func(payload *localNodeReadinessPayload) { payload.Nodes[0].SummaryFresh = false }},
		{name: "axnoded not ready", mutate: func(payload *localNodeReadinessPayload) { payload.Nodes[0].Summary.Components.Axnoded.Ready = false }},
		{name: "imagemgr unreachable", mutate: func(payload *localNodeReadinessPayload) {
			payload.Nodes[0].Summary.Components.Imagemgr.Reachable = false
		}},
		{name: "runtime slots absent", mutate: func(payload *localNodeReadinessPayload) { payload.Nodes[0].Summary.Pools.RuntimeSlots = nil }},
		{name: "capability snapshot warming", mutate: func(payload *localNodeReadinessPayload) { payload.Nodes[0].Summary.CapabilitySnapshot.Sequence = 0 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := readyLocalNodePayload(time.Now().Add(time.Minute))
			test.mutate(&payload)
			if ready, reason := evaluateLocalNodeReadiness(payload, LocalNodeID, localDefaultWorkloadCapabilities, time.Now()); ready || reason == "" {
				t.Fatalf("incomplete node result = ready:%t reason:%q", ready, reason)
			}
		})
	}
}

func TestLocalNodeReadinessPayloadDecodesDebugCapabilityKeys(t *testing.T) {
	const body = `{"nodes":[{"node_id":"node-local","fresh":true,"summary_fresh":true,"summary":{"components":{"axnoded":{"ready":true},"imagemgr":{"reachable":true}},"pools":{"runtime_slots":{"capacity":16}},"capability_snapshot":{"sequence":1,"observations":[{"key":{"Kind":{"Platform":2}},"state":1},{"key":{"Kind":{"Platform":10}},"state":1}]}}}]}`
	var payload localNodeReadinessPayload
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if ready, reason := evaluateLocalNodeReadiness(payload, LocalNodeID, localDefaultWorkloadCapabilities, time.Now()); !ready {
		t.Fatalf("decoded ready payload rejected: %s", reason)
	}
}

func readyLocalNodePayload(validUntil time.Time) localNodeReadinessPayload {
	var payload localNodeReadinessPayload
	payload.Nodes = make([]struct {
		NodeID       string `json:"node_id"`
		Fresh        bool   `json:"fresh"`
		SummaryFresh bool   `json:"summary_fresh"`
		Summary      struct {
			Components struct {
				Axnoded struct {
					Ready bool `json:"ready"`
				} `json:"axnoded"`
				Imagemgr struct {
					Reachable bool `json:"reachable"`
				} `json:"imagemgr"`
			} `json:"components"`
			Pools struct {
				RuntimeSlots *struct {
					Capacity int64 `json:"capacity"`
				} `json:"runtime_slots"`
			} `json:"pools"`
			CapabilitySnapshot struct {
				Sequence     uint64                       `json:"sequence"`
				Observations []localCapabilityObservation `json:"observations"`
			} `json:"capability_snapshot"`
		} `json:"summary"`
	}, 1)
	node := &payload.Nodes[0]
	node.NodeID = LocalNodeID
	node.Fresh = true
	node.SummaryFresh = true
	node.Summary.Components.Axnoded.Ready = true
	node.Summary.Components.Imagemgr.Reachable = true
	node.Summary.Pools.RuntimeSlots = &struct {
		Capacity int64 `json:"capacity"`
	}{Capacity: 16}
	node.Summary.CapabilitySnapshot.Sequence = 1
	for _, platform := range localDefaultWorkloadCapabilities {
		var observation localCapabilityObservation
		observation.Key.Kind.Platform = platform
		observation.State = capabilityv1.CapabilityState_CAPABILITY_STATE_AVAILABLE
		observation.ValidUntil = &struct {
			Seconds int64 `json:"seconds"`
			Nanos   int32 `json:"nanos"`
		}{Seconds: validUntil.Unix(), Nanos: int32(validUntil.Nanosecond())}
		node.Summary.CapabilitySnapshot.Observations = append(node.Summary.CapabilitySnapshot.Observations, observation)
	}
	return payload
}
