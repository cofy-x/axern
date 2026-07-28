package imageregistry

import (
	"runtime"
	"testing"
)

func TestWithRegistryMirrorRejectsNonOriginURL(t *testing.T) {
	for _, rawURL := range []string{"ftp://mirror.example", "http://mirror.example/path", "http://user@mirror.example"} {
		if _, err := NewClient("", WithRegistryMirror(rawURL)); err == nil {
			t.Fatalf("NewClient() accepted mirror URL %q", rawURL)
		}
	}
}

func TestDefaultRemotePlatformMatchesCurrentRuntime(t *testing.T) {
	platform := defaultRemotePlatform()
	if platform.OS != "linux" {
		t.Fatalf("platform OS = %q, want linux", platform.OS)
	}
	if platform.Architecture != runtime.GOARCH {
		t.Fatalf("platform architecture = %q, want %q", platform.Architecture, runtime.GOARCH)
	}
}

func TestClientUsesHTTPForConfiguredInsecureRegistry(t *testing.T) {
	t.Setenv("IMAGEMGR_INSECURE_REGISTRIES", "localhost:5001, http://host.docker.internal:5001/ ")

	client, err := NewClient("")
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	for _, imageRef := range []string{
		"localhost:5001/axern/python:dev",
		"http://localhost:5001/axern/python:dev",
		"host.docker.internal:5001/axern/python:dev",
	} {
		if !client.useHTTPFor(imageRef) {
			t.Fatalf("useHTTPFor(%q) = false, want true", imageRef)
		}
	}
	if client.useHTTPFor("docker.io/library/python:3.12") {
		t.Fatal("useHTTPFor(docker.io/library/python:3.12) = true, want false")
	}
}

func TestRegistryHost(t *testing.T) {
	tests := map[string]string{
		"localhost:5001/axern/python:dev":              "localhost:5001",
		"host.docker.internal:5001/axern/python:dev":   "host.docker.internal:5001",
		"ghcr.io/dragonflyoss/image-service/nginx:dev": "ghcr.io",
		"python:3.12-slim":                             "index.docker.io",
	}

	for imageRef, want := range tests {
		if got := registryHost(imageRef); got != want {
			t.Fatalf("registryHost(%q) = %q, want %q", imageRef, got, want)
		}
	}
}
