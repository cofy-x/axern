package oci

import (
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func TestPlatformEnvironmentDirectAndTemplate(t *testing.T) {
	for _, term := range []string{"", "image-term", "caller-term"} {
		t.Run(term, func(t *testing.T) {
			root := t.TempDir()
			loader, err := NewBundleLoader(root)
			if err != nil {
				t.Fatal(err)
			}
			before, err := loader.ConfigurationDigest()
			if err != nil {
				t.Fatal(err)
			}
			// Arbitrary node files cannot change a loaded policy or its evidence.
			if err := os.WriteFile(filepath.Join(root, "runsc-config.json"), []byte(`{"process":{"env":["TERM=xterm"]}}`), 0600); err != nil {
				t.Fatal(err)
			}
			request := &apipb.CreateContainerRequest{Command: []string{"/bin/true"}, Rootfs: &apipb.Rootfs{RootDir: root}}
			if term != "" {
				request.Envs = append(request.Envs, &apipb.KeyValue{Key: "TERM", Value: "image-term"})
			}
			if term == "caller-term" {
				request.Envs = append(request.Envs, &apipb.KeyValue{Key: "TERM", Value: term})
			}
			template, err := loader.PrepareBundleTemplate(TemplateOptions{Request: request})
			if err != nil {
				t.Fatal(err)
			}
			var direct *spec.Spec
			for _, id := range []string{"allocation-direct", "allocation-template"} {
				opts := LoadOptions{ContainerID: id, Request: request}
				var s *spec.Spec
				if id == "allocation-direct" {
					_, s, err = loader.Generate(opts)
				} else {
					_, s, err = loader.MaterializeBundle(template, opts)
				}
				if err != nil {
					t.Fatal(err)
				}
				if s.Process.Terminal {
					t.Fatal("workload acquired a terminal")
				}
				if term == "" && hasEnv(s.Process.Env, "TERM") {
					t.Fatalf("synthetic TERM: %v", s.Process.Env)
				}
				if term != "" && !hasEnvValue(s.Process.Env, "TERM", term) {
					t.Fatalf("lost explicit TERM: %v", s.Process.Env)
				}
				slices.Sort(s.Process.Env) // environment ordering is not semantic
				if direct == nil {
					direct = s
				} else if !reflect.DeepEqual(direct.Process, s.Process) {
					t.Fatal("direct and template process contracts differ")
				}
			}
			after, err := loader.ConfigurationDigest()
			if err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatal("materialization or unrelated file changed loaded configuration")
			}
		})
	}
}

func TestLoaderOwnsStartupOptions(t *testing.T) {
	profile := DefaultExecutionProfile()
	dns := RuntimeDNSConfig{Nameservers: []string{"1.1.1.1"}}
	loader, err := NewBundleLoader(t.TempDir(), WithExecutionProfile(profile), WithRuntimeDNSConfig(dns))
	if err != nil {
		t.Fatal(err)
	}
	before, err := loader.ConfigurationDigest()
	if err != nil {
		t.Fatal(err)
	}
	profile.Baseline.Capabilities[0] = "CAP_SYS_ADMIN"
	dns.Nameservers[0] = "8.8.8.8"
	after, err := loader.ConfigurationDigest()
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("caller mutated loaded configuration")
	}
}
