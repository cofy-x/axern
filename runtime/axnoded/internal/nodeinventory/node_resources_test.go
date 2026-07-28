package nodeinventory

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestResourcesFromKubernetesNodeUsesCapacityAndAllocatable(t *testing.T) {
	var node kubernetesNodeResourceResponse
	node.Status.Capacity = map[string]string{
		"cpu":    "2",
		"memory": "3648988Ki",
	}
	node.Status.Allocatable = map[string]string{
		"cpu":    "1930m",
		"memory": "2430428Ki",
	}
	node.Metadata.Labels = map[string]string{
		"kubernetes.io/hostname":      "node-a",
		"topology.kubernetes.io/zone": "zone-a",
	}

	resources, err := resourcesFromKubernetesNode(node)
	if err != nil {
		t.Fatalf("resourcesFromKubernetesNode() error = %v", err)
	}
	if got, want := resources.Capacity.CpuMilli, int64(2000); got != want {
		t.Fatalf("capacity cpu = %d, want %d", got, want)
	}
	if got, want := resources.Capacity.MemoryBytes, int64(3648988*1024); got != want {
		t.Fatalf("capacity memory = %d, want %d", got, want)
	}
	if got, want := resources.Allocatable.CpuMilli, int64(1930); got != want {
		t.Fatalf("allocatable cpu = %d, want %d", got, want)
	}
	if got, want := resources.Allocatable.MemoryBytes, int64(2430428*1024); got != want {
		t.Fatalf("allocatable memory = %d, want %d", got, want)
	}
	if got, want := resources.Labels["kubernetes.io/hostname"], "node-a"; got != want {
		t.Fatalf("hostname label = %q, want %q", got, want)
	}
}

func TestAxnodedSourceMergesKubernetesAndConfiguredNodeLabels(t *testing.T) {
	provider := &flakyNodeResourceProvider{resources: NodeResources{
		Capacity:    NodeResourceQuantity{CpuMilli: 2000, MemoryBytes: 4 << 30},
		Allocatable: NodeResourceQuantity{CpuMilli: 1930, MemoryBytes: 2 << 30},
		Labels: map[string]string{
			"kubernetes.io/hostname":      "node-a",
			"topology.kubernetes.io/zone": "zone-from-kubernetes",
		},
	}}
	source := NewAxnodedSource(AxnodedSourceOptions{
		NodeResources: provider,
		NodeLabels: map[string]string{
			"topology.kubernetes.io/zone": "zone-explicit",
			"axern.cofy.io/pool":          "runtime",
		},
		Ready:        func() bool { return true },
		RuntimeCount: func() int { return 1 },
		Container: &fakeContainerManager{pools: map[string]PoolInventory{
			"cgroup": {Capacity: 8}, "interface": {Capacity: 8},
		}},
	})

	snapshot, ready := source.Collect(context.Background())
	if !ready {
		t.Fatal("expected inventory to be ready")
	}
	if got := snapshot.Node.Labels["kubernetes.io/hostname"]; got != "node-a" {
		t.Fatalf("hostname label = %q, want node-a", got)
	}
	if got := snapshot.Node.Labels["topology.kubernetes.io/zone"]; got != "zone-explicit" {
		t.Fatalf("explicit zone label = %q, want zone-explicit", got)
	}
	if got := snapshot.Node.Labels["axern.cofy.io/pool"]; got != "runtime" {
		t.Fatalf("pool label = %q, want runtime", got)
	}
}

func TestParseKubernetesResourceQuantities(t *testing.T) {
	for _, tc := range []struct {
		name  string
		parse func(string) (int64, error)
		value string
		want  int64
	}{
		{name: "cpu cores", parse: parseKubernetesCPUQuantity, value: "2", want: 2000},
		{name: "cpu milli", parse: parseKubernetesCPUQuantity, value: "1930m", want: 1930},
		{name: "memory kibibytes", parse: parseKubernetesMemoryQuantity, value: "2430428Ki", want: 2430428 * 1024},
		{name: "memory gibibytes", parse: parseKubernetesMemoryQuantity, value: "2Gi", want: 2 * 1024 * 1024 * 1024},
		{name: "memory bytes", parse: parseKubernetesMemoryQuantity, value: "512", want: 512},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.parse(tc.value)
			if err != nil {
				t.Fatalf("parse(%q) error = %v", tc.value, err)
			}
			if got != tc.want {
				t.Fatalf("parse(%q) = %d, want %d", tc.value, got, tc.want)
			}
		})
	}
}

func TestParseKubernetesResourceQuantitiesRejectFractions(t *testing.T) {
	if _, err := parseKubernetesCPUQuantity("1/2"); err == nil {
		t.Fatal("expected fractional cpu quantity to fail")
	}
	if _, err := parseKubernetesMemoryQuantity("1/2Gi"); err == nil {
		t.Fatal("expected fractional memory quantity to fail")
	}
}

func TestAxnodedSourceNodeResourceProviderFailureBlocksFirstReport(t *testing.T) {
	source := NewAxnodedSource(AxnodedSourceOptions{
		NodeResources: NewErrorNodeResourceProvider(errors.New("kubernetes node api unavailable")),
		Container:     &fakeContainerManager{},
	})

	snapshot, ready := source.Collect(context.Background())
	if ready {
		t.Fatal("expected inventory to be not ready without node resources")
	}
	if snapshot.Sources["node_resources"].Status != StatusError {
		t.Fatalf("node resource source = %#v, want error", snapshot.Sources["node_resources"])
	}
}

func TestAxnodedSourceKeepsLastNodeResourcesDuringProviderDegradation(t *testing.T) {
	provider := &flakyNodeResourceProvider{
		resources: NodeResources{
			Capacity:    NodeResourceQuantity{CpuMilli: 2000, MemoryBytes: 4 << 30},
			Allocatable: NodeResourceQuantity{CpuMilli: 1930, MemoryBytes: 2 << 30},
		},
	}
	source := NewAxnodedSource(AxnodedSourceOptions{
		NodeResources: provider,
		Ready:         func() bool { return true },
		RuntimeCount:  func() int { return 1 },
		Container: &fakeContainerManager{
			pools: map[string]PoolInventory{
				"cgroup":    {Capacity: 8},
				"interface": {Capacity: 8},
			},
		},
	})

	snapshot, ready := source.Collect(context.Background())
	if !ready {
		t.Fatal("expected first inventory to be ready")
	}
	if got, want := snapshot.Node.Allocatable.CpuMilli, int64(1930); got != want {
		t.Fatalf("allocatable cpu = %d, want %d", got, want)
	}

	provider.err = errors.New("temporary kubernetes api error")
	snapshot, ready = source.Collect(context.Background())
	if !ready {
		t.Fatal("expected cached node resources to keep inventory reportable")
	}
	if got, want := snapshot.Node.Allocatable.MemoryBytes, int64(2<<30); got != want {
		t.Fatalf("cached allocatable memory = %d, want %d", got, want)
	}
	if snapshot.Sources["node_resources"].Status != StatusDegraded {
		t.Fatalf("node resource source = %#v, want degraded", snapshot.Sources["node_resources"])
	}
}

type flakyNodeResourceProvider struct {
	resources NodeResources
	err       error
}

func (p *flakyNodeResourceProvider) Collect(context.Context) (NodeResources, error) {
	if p.err != nil {
		return NodeResources{}, p.err
	}
	return p.resources, nil
}

func TestKubernetesNodeResourceProviderRequiresNodeName(t *testing.T) {
	if _, err := NewKubernetesNodeResourceProvider(KubernetesNodeResourceProviderOptions{}); err == nil {
		t.Fatal("expected missing node name to fail")
	}
}

func TestDefaultNodeResourceProviderUsesHostProvider(t *testing.T) {
	provider := defaultNodeResourceProvider(nil)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	resources, err := provider.Collect(ctx)
	if err != nil {
		t.Fatalf("host provider collect error = %v", err)
	}
	if resources.Capacity.CpuMilli <= 0 || resources.Allocatable.CpuMilli != resources.Capacity.CpuMilli {
		t.Fatalf("unexpected host resources = %#v", resources)
	}
}
