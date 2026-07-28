package parse

import "testing"

func TestCPU(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int64
	}{
		{value: "", want: 0},
		{value: "500m", want: 500},
		{value: "1", want: 1000},
		{value: "1.5", want: 1500},
		{value: ".25", want: 250},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got, err := CPU(tc.value)
			if err != nil {
				t.Fatalf("CPU returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestMemory(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  int64
	}{
		{value: "", want: 0},
		{value: "128Mi", want: 128 * 1024 * 1024},
		{value: "512MiB", want: 512 * 1024 * 1024},
		{value: "1Gi", want: 1024 * 1024 * 1024},
		{value: "1GiB", want: 1024 * 1024 * 1024},
		{value: "1.5GB", want: 1500 * 1000 * 1000},
		{value: "2Ki", want: 2 * 1024},
		{value: "128", want: 128},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got, err := Memory(tc.value)
			if err != nil {
				t.Fatalf("Memory returned error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
}

func TestResourcesRejectInvalidValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{name: "negative cpu", run: func() error { _, err := CPU("-1"); return err }},
		{name: "fractional milli cpu", run: func() error { _, err := CPU("0.5m"); return err }},
		{name: "negative memory", run: func() error { _, err := Memory("-1MiB"); return err }},
		{name: "fractional byte", run: func() error { _, err := Memory("0.5B"); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
