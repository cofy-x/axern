package axernsdk

import "testing"

func TestBuildResourceSpecParsesFriendlyQuantities(t *testing.T) {
	resources, err := buildResourceSpec(CPUMilli(500), "512Mi", "1.5", "1GiB")
	if err != nil {
		t.Fatalf("buildResourceSpec returned error: %v", err)
	}
	if got, want := resources.GetRequests().GetCpuMilli(), int64(500); got != want {
		t.Fatalf("got request cpu %d, want %d", got, want)
	}
	if got, want := resources.GetRequests().GetMemoryBytes(), int64(512*1024*1024); got != want {
		t.Fatalf("got request memory %d, want %d", got, want)
	}
	if got, want := resources.GetLimits().GetCpuMilli(), int64(1500); got != want {
		t.Fatalf("got limit cpu %d, want %d", got, want)
	}
	if got, want := resources.GetLimits().GetMemoryBytes(), int64(1024*1024*1024); got != want {
		t.Fatalf("got limit memory %d, want %d", got, want)
	}
}

func TestBuildResourceSpecParsesBinaryMemorySuffixVariants(t *testing.T) {
	resources, err := buildResourceSpec("", "128Mi", "", "2Ki")
	if err != nil {
		t.Fatalf("buildResourceSpec returned error: %v", err)
	}
	if got, want := resources.GetRequests().GetMemoryBytes(), int64(128*1024*1024); got != want {
		t.Fatalf("got request memory %d, want %d", got, want)
	}
	if got, want := resources.GetLimits().GetMemoryBytes(), int64(2*1024); got != want {
		t.Fatalf("got limit memory %d, want %d", got, want)
	}
}

func TestResourceQuantityConstructors(t *testing.T) {
	resources, err := buildResourceSpec(CPUCores(2), MemoryBytes(512), CPUMilli(750), MemoryBytes(1024))
	if err != nil {
		t.Fatalf("buildResourceSpec returned error: %v", err)
	}
	if got, want := resources.GetRequests().GetCpuMilli(), int64(2000); got != want {
		t.Fatalf("got request cpu %d, want %d", got, want)
	}
	if got, want := resources.GetRequests().GetMemoryBytes(), int64(512); got != want {
		t.Fatalf("got request memory %d, want %d", got, want)
	}
	if got, want := resources.GetLimits().GetCpuMilli(), int64(750); got != want {
		t.Fatalf("got limit cpu %d, want %d", got, want)
	}
	if got, want := resources.GetLimits().GetMemoryBytes(), int64(1024); got != want {
		t.Fatalf("got limit memory %d, want %d", got, want)
	}
}

func TestBuildResourceSpecRejectsInvalidQuantities(t *testing.T) {
	for _, tc := range []struct {
		name          string
		requestCPU    ResourceQuantity
		requestMemory ResourceQuantity
		limitCPU      ResourceQuantity
		limitMemory   ResourceQuantity
	}{
		{name: "negative request cpu", requestCPU: "-1"},
		{name: "negative request memory", requestMemory: "-1"},
		{name: "negative limit cpu", limitCPU: "-1"},
		{name: "negative limit memory", limitMemory: "-1"},
		{name: "fractional milli cpu", requestCPU: "0.5m"},
		{name: "fractional byte", requestMemory: "0.5B"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := buildResourceSpec(tc.requestCPU, tc.requestMemory, tc.limitCPU, tc.limitMemory); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
