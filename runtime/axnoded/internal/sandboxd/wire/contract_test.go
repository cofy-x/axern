package wire

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestCapabilityContract(t *testing.T) {
	wantBaseline := []string{
		CapabilityArchive,
		CapabilityDiagnostics,
		CapabilityFile,
		CapabilityHealth,
		CapabilityManagedProxy,
		CapabilityMounts,
		CapabilityPorts,
		CapabilityProbe,
		CapabilityProcess,
		CapabilityPTY,
		CapabilityStatus,
		CapabilitySupervisor,
	}
	if got := BaselineCapabilities(); !reflect.DeepEqual(got, wantBaseline) {
		t.Fatalf("BaselineCapabilities() = %#v, want %#v", got, wantBaseline)
	}
	wantOptional := []string{CapabilityBrowser, CapabilityComputerUse}
	if got := OptionalCapabilities(); !reflect.DeepEqual(got, wantOptional) {
		t.Fatalf("OptionalCapabilities() = %#v, want %#v", got, wantOptional)
	}
	assertSortedUnique(t, "baseline", BaselineCapabilities())
	assertSortedUnique(t, "optional", OptionalCapabilities())
	assertDisjoint(t, BaselineCapabilities(), OptionalCapabilities())
}

func TestProviderGroupContract(t *testing.T) {
	cases := []struct {
		name string
		got  []string
		want []string
	}{
		{
			name: "core",
			got:  CoreCapabilities(),
			want: []string{CapabilityDiagnostics, CapabilityHealth, CapabilityMounts, CapabilityPorts, CapabilityProbe, CapabilityStatus, CapabilitySupervisor},
		},
		{
			name: "file",
			got:  FileCapabilities(),
			want: []string{CapabilityArchive, CapabilityFile},
		},
		{
			name: "process",
			got:  ProcessCapabilities(),
			want: []string{CapabilityManagedProxy, CapabilityProcess, CapabilityPTY},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !reflect.DeepEqual(tc.got, tc.want) {
				t.Fatalf("capabilities = %#v, want %#v", tc.got, tc.want)
			}
			assertSortedUnique(t, tc.name, tc.got)
		})
	}
}

func TestCoreResponseProtocolVersionContract(t *testing.T) {
	generatedAt := time.Unix(1, 0).UTC()
	status := StatusResponse{
		ProtocolVersion: ProtocolVersion,
		DaemonPID:       7,
		UptimeSeconds:   1.5,
		SocketPath:      "/mnt/axern-sandboxd.sock",
		UserProcess:     UserProcessStatus{State: "running", PID: 11},
	}
	responses := map[string]any{
		"health": HealthResponse{ProtocolVersion: ProtocolVersion, Status: "ok"},
		"ready":  ReadyResponse{ProtocolVersion: ProtocolVersion, Ready: true},
		"capabilities": CapabilitiesResponse{
			ProtocolVersion: ProtocolVersion,
			Capabilities:    BaselineCapabilities(),
			Providers:       []CapabilityProvider{{Name: "core", State: ProviderStateAvailable, Available: true, Capabilities: CoreCapabilities()}},
			Summary:         ProviderSummary{Total: 1, Available: 1},
		},
		"status": status,
		"diagnostics": DiagnosticsResponse{
			ProtocolVersion: ProtocolVersion,
			GeneratedAt:     generatedAt,
			Ready:           true,
			Status:          status,
			Capabilities:    BaselineCapabilities(),
			ProviderSummary: ProviderSummary{Total: 1, Available: 1},
		},
	}
	for name, response := range responses {
		t.Run(name, func(t *testing.T) {
			data, err := json.Marshal(response)
			if err != nil {
				t.Fatalf("Marshal() error = %v", err)
			}
			var envelope struct {
				ProtocolVersion int `json:"protocolVersion"`
			}
			if err := json.Unmarshal(data, &envelope); err != nil {
				t.Fatalf("Unmarshal() error = %v", err)
			}
			if envelope.ProtocolVersion != ProtocolVersion {
				t.Fatalf("protocolVersion = %d, want %d; json=%s", envelope.ProtocolVersion, ProtocolVersion, data)
			}
		})
	}
}

func TestErrorResponseContract(t *testing.T) {
	data, err := json.Marshal(ErrorResponse{Error: ResponseError{Code: ErrorCodeInvalidArgument, Message: "bad request"}})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if got, want := string(data), `{"error":{"code":"invalid_argument","message":"bad request"}}`; got != want {
		t.Fatalf("error json = %s, want %s", got, want)
	}
}

func assertSortedUnique(t *testing.T, name string, values []string) {
	t.Helper()
	if !sort.StringsAreSorted(values) {
		t.Fatalf("%s capabilities are not sorted: %#v", name, values)
	}
	seen := map[string]struct{}{}
	for _, value := range values {
		if value == "" {
			t.Fatalf("%s capabilities contain an empty value: %#v", name, values)
		}
		if _, ok := seen[value]; ok {
			t.Fatalf("%s capabilities contain duplicate %q: %#v", name, value, values)
		}
		seen[value] = struct{}{}
	}
}

func assertDisjoint(t *testing.T, left, right []string) {
	t.Helper()
	seen := map[string]struct{}{}
	for _, value := range left {
		seen[value] = struct{}{}
	}
	for _, value := range right {
		if _, ok := seen[value]; ok {
			t.Fatalf("baseline and optional capabilities overlap at %q", value)
		}
	}
}
