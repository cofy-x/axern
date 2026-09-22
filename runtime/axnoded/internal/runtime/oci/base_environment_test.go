package oci

import (
	"path/filepath"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
)

func TestConfiguredBaseEnvironmentBeforeWorkloadOverrides(t *testing.T) {
	for _, stale := range []bool{false, true} {
		for _, term := range []string{"", "image-term", "caller-term"} {
			t.Run(term+map[bool]string{false: "/generated", true: "/existing"}[stale], func(t *testing.T) {
				root := t.TempDir()
				base := filepath.Join(root, "base.json")
				if err := WriteBaseSpec(base); err != nil {
					t.Fatal(err)
				}
				if stale {
					s, err := LoadSpec(base)
					if err != nil {
						t.Fatal(err)
					}
					s.Process.Terminal = true
					s.Process.Env = append(s.Process.Env, "TERM=xterm")
					if err := WriteSpecAtomic(base, s); err != nil {
						t.Fatal(err)
					}
				}
				for boot := 0; boot < 2; boot++ {
					loader, err := NewBundleLoader(base, filepath.Join(root, "containers"))
					if err != nil {
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
					for _, cached := range []bool{false, true} {
						opts := LoadOptions{ContainerID: "allocation", Request: request}
						_, s, err := loader.Generate(opts)
						if cached {
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
					}
					if err := WriteBaseSpec(base); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}
