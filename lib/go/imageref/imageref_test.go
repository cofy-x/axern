package imageref

import "testing"

func TestNormalize(t *testing.T) {
	tests := map[string]string{
		" python:3.12-slim ":                        "python:3.12-slim",
		"http://localhost:5001/axern/runtime:dev":   "localhost:5001/axern/runtime:dev",
		"https://ghcr.io/dragonflyoss/image:latest": "ghcr.io/dragonflyoss/image:latest",
		"host.docker.internal:5001/axern/image:dev": "host.docker.internal:5001/axern/image:dev",
	}
	for input, want := range tests {
		if got := Normalize(input); got != want {
			t.Fatalf("Normalize(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRegistryHost(t *testing.T) {
	tests := map[string]string{
		"python:3.12-slim":                            DefaultRegistry,
		"library/python:3.12-slim":                    DefaultRegistry,
		"localhost:5001/axern/python:dev":             "localhost:5001",
		"https://host.docker.internal:5001/axern/dev": "host.docker.internal:5001",
		"ghcr.io/dragonflyoss/image-service/nginx":    "ghcr.io",
	}
	for input, want := range tests {
		if got := RegistryHost(input); got != want {
			t.Fatalf("RegistryHost(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestWithDigestRemovesMutableTagWithoutRemovingRegistryPort(t *testing.T) {
	const digest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	got, err := WithDigest("localhost:5001/team/runtime:latest", digest)
	if err != nil {
		t.Fatalf("WithDigest() error = %v", err)
	}
	if want := "localhost:5001/team/runtime@" + digest; got != want {
		t.Fatalf("WithDigest() = %q, want %q", got, want)
	}
}

func TestWithDigestReplacesExistingDigest(t *testing.T) {
	const digest = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	got, err := WithDigest("registry.example/team/runtime@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", digest)
	if err != nil {
		t.Fatalf("WithDigest() error = %v", err)
	}
	if want := "registry.example/team/runtime@" + digest; got != want {
		t.Fatalf("WithDigest() = %q, want %q", got, want)
	}
}

func TestUseHTTPFor(t *testing.T) {
	registries := HostSetFromCSV("localhost:5001, http://host.docker.internal:5001/")
	for _, input := range []string{
		"localhost:5001/axern/runtime:dev",
		"http://host.docker.internal:5001/axern/runtime:dev",
	} {
		if !UseHTTPFor(input, registries) {
			t.Fatalf("UseHTTPFor(%q) = false, want true", input)
		}
	}
	if UseHTTPFor("python:3.12-slim", registries) {
		t.Fatal("UseHTTPFor(docker hub ref) = true, want false")
	}
}
