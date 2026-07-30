package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseMemorySize(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want int64
	}{
		{name: "empty", in: "", want: 0},
		{name: "zero", in: "0", want: 0},
		{name: "bytes", in: "2048", want: 2048},
		{name: "binary mib", in: "2MiB", want: 2 * 1024 * 1024},
		{name: "decimal mb", in: "2MB", want: 2 * 1000 * 1000},
		{name: "fractional gib", in: "1.5GiB", want: int64(1.5 * 1024 * 1024 * 1024)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMemorySize(tt.in)
			if err != nil {
				t.Fatalf("parseMemorySize() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseMemorySize() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestParseMemorySizeRejectsUnknownUnit(t *testing.T) {
	if _, err := parseMemorySize("10xb"); err == nil {
		t.Fatal("parseMemorySize() error = nil, want error")
	}
}

func TestRegistryProxyURLFromTemplate(t *testing.T) {
	templatePath := filepath.Join(t.TempDir(), "nydus-template.json")
	if err := os.WriteFile(templatePath, []byte(`{
  "type": "registry",
  "registry": {
    "scheme": "https",
    "host": "registry.invalid",
    "repo": "placeholder/image",
    "proxy": {
      "url": "http://proxy.example.test:18080",
      "fallback": true
    }
  }
}`), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	got, err := registryProxyURLFromTemplate(templatePath)
	if err != nil {
		t.Fatalf("registryProxyURLFromTemplate() error = %v", err)
	}
	if got != "http://proxy.example.test:18080" {
		t.Fatalf("registryProxyURLFromTemplate() = %q, want proxy URL", got)
	}
}

func TestRegistryProxyURLFromTemplateEmptyPath(t *testing.T) {
	got, err := registryProxyURLFromTemplate("")
	if err != nil {
		t.Fatalf("registryProxyURLFromTemplate() error = %v", err)
	}
	if got != "" {
		t.Fatalf("registryProxyURLFromTemplate() = %q, want empty", got)
	}
}
