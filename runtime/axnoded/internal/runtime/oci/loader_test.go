package oci

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	resourcemanager "github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/workloadidentity"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func newTestBundleLoader(t *testing.T, baseFile, bundleDir string, options ...BundleLoaderOption) (*BundleLoader, error) {
	t.Helper()
	loader, err := NewBundleLoader(baseFile, bundleDir, options...)
	if err != nil {
		return nil, err
	}
	if loader.baseSpec.Annotations == nil {
		loader.baseSpec.Annotations = map[string]string{}
	}
	key := resourcemanager.ResourceAnnotationKeyPrefix + string(resourcemanager.InterfaceResourceName)
	loader.baseSpec.Annotations[key] = (&resourcemanager.NetResource{Ip: net.ParseIP("10.88.0.2")}).ToString()
	return loader, nil
}

func TestNewBundleLoaderRejectsMissingConfiguredBaseSpec(t *testing.T) {
	if _, err := NewBundleLoader(filepath.Join(t.TempDir(), "missing.json"), t.TempDir()); err == nil {
		t.Fatal("NewBundleLoader accepted a missing configured base spec")
	}
}

func TestCombineEnvs(t *testing.T) {
	tests := []struct {
		name    string
		envs    []string
		request *apipb.CreateContainerRequest
		want    []string
	}{
		{
			name: "append new env",
			envs: []string{"a=1", "b=2"},
			request: &apipb.CreateContainerRequest{
				Envs: []*runtimeapi.KeyValue{{Key: "c", Value: "3"}},
			},
			want: []string{"a=1", "b=2", "c=3"},
		},
		{
			name: "override existing env",
			envs: []string{"a=1", "b=2"},
			request: &apipb.CreateContainerRequest{
				Envs: []*runtimeapi.KeyValue{{Key: "a", Value: "3"}},
			},
			want: []string{"a=3", "b=2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := combineEnvs(tt.envs, tt.request)
			sort.Strings(got)
			sort.Strings(tt.want)
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("combineEnvs() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGenerateAllowsEmptyCgroupPath(t *testing.T) {
	loader, err := newTestBundleLoader(t, "", t.TempDir())
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	bundleDir, spec, err := loader.Generate(LoadOptions{
		ContainerID: "test-no-cgroup",
		Request: &apipb.CreateContainerRequest{
			Command: []string{"/bin/true"},
			Rootfs:  &apipb.Rootfs{RootDir: t.TempDir()},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if bundleDir == "" {
		t.Fatal("Generate() returned empty bundle dir")
	}
	if spec == nil || spec.Linux == nil {
		t.Fatal("Generate() returned nil linux spec")
	}
	if spec.Linux.CgroupsPath != "" {
		t.Fatalf("Generate() cgroupsPath = %q, want empty", spec.Linux.CgroupsPath)
	}
}

func TestGenerateAddsRequestedLinuxCapabilities(t *testing.T) {
	loader, err := newTestBundleLoader(t, "", t.TempDir())
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	_, spec, err := loader.Generate(LoadOptions{
		ContainerID: "test-capabilities",
		Request: &apipb.CreateContainerRequest{
			Command: []string{"/bin/true"},
			Rootfs:  &apipb.Rootfs{RootDir: t.TempDir()},
			Labels: map[string]string{
				linuxCapabilitiesAnnoKey: "CAP_NET_RAW,CAP_NET_BIND_SERVICE,CAP_NET_RAW",
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if spec == nil || spec.Process == nil || spec.Process.Capabilities == nil {
		t.Fatalf("Generate() returned nil process capabilities")
	}

	assertHas := func(name string, values []string) {
		t.Helper()
		for _, value := range values {
			if value == name {
				return
			}
		}
		t.Fatalf("capability %q missing from %v", name, values)
	}

	assertHas("CAP_NET_RAW", spec.Process.Capabilities.Bounding)
	assertHas("CAP_NET_RAW", spec.Process.Capabilities.Effective)
	assertHas("CAP_NET_RAW", spec.Process.Capabilities.Inheritable)
	assertHas("CAP_NET_RAW", spec.Process.Capabilities.Permitted)
	assertHas("CAP_NET_RAW", spec.Process.Capabilities.Ambient)
}

func TestGenerateRejectsMissingProcessArgs(t *testing.T) {
	loader, err := newTestBundleLoader(t, "", t.TempDir())
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	_, _, err = loader.Generate(LoadOptions{
		ContainerID: "test-missing-command",
		Request: &apipb.CreateContainerRequest{
			Rootfs: &apipb.Rootfs{RootDir: t.TempDir()},
		},
	})
	if err == nil {
		t.Fatal("Generate() error = nil, want invalid argument for missing command")
	}
}

func TestGeneratePreservesCustomBaseProcessArgsWhenCommandEmpty(t *testing.T) {
	baseFile := filepath.Join(t.TempDir(), "config.json")
	base := defaultBundleSpec()
	base.Process.Args = []string{"/image-entrypoint", "serve"}
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal base spec: %v", err)
	}
	if err := os.WriteFile(baseFile, data, 0644); err != nil {
		t.Fatalf("write base spec: %v", err)
	}
	loader, err := newTestBundleLoader(t, baseFile, t.TempDir())
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	_, generated, err := loader.Generate(LoadOptions{
		ContainerID: "test-custom-base-command",
		Request: &apipb.CreateContainerRequest{
			Rootfs: &apipb.Rootfs{RootDir: t.TempDir()},
			Cwd:    "/app",
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !reflect.DeepEqual(generated.Process.Args, []string{"/image-entrypoint", "serve"}) {
		t.Fatalf("process args = %#v, want custom base image/default args", generated.Process.Args)
	}
	if got := generated.Process.Cwd; got != "/app" {
		t.Fatalf("cwd = %q, want /app", got)
	}
}

func TestGenerateOverridesBaseProcessArgsWhenCommandProvided(t *testing.T) {
	loader, err := newTestBundleLoader(t, "", t.TempDir())
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	_, generated, err := loader.Generate(LoadOptions{
		ContainerID: "test-command-override",
		Request: &apipb.CreateContainerRequest{
			Command: []string{"/bin/sh", "-lc", "sleep 60"},
			Rootfs:  &apipb.Rootfs{RootDir: t.TempDir()},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got := generated.Process.Args; !reflect.DeepEqual(got, []string{"/bin/sh", "-lc", "sleep 60"}) {
		t.Fatalf("process args = %#v, want requested command", got)
	}
}

func TestGenerateRaisesManagedProcessRuntimeBaseline(t *testing.T) {
	baseFile := filepath.Join(t.TempDir(), "config.json")
	base := defaultBundleSpec()
	base.Process.Args = []string{"/custom"}
	base.Process.Rlimits = []spec.POSIXRlimit{{Type: "RLIMIT_NOFILE", Soft: 1024, Hard: 1024}}
	base.Process.Capabilities = &spec.LinuxCapabilities{
		Bounding:    []string{"CAP_AUDIT_WRITE", "CAP_KILL", "CAP_NET_BIND_SERVICE"},
		Effective:   []string{"CAP_AUDIT_WRITE", "CAP_KILL", "CAP_NET_BIND_SERVICE"},
		Inheritable: []string{"CAP_AUDIT_WRITE", "CAP_KILL", "CAP_NET_BIND_SERVICE"},
		Permitted:   []string{"CAP_AUDIT_WRITE", "CAP_KILL", "CAP_NET_BIND_SERVICE"},
	}
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal base spec: %v", err)
	}
	if err := os.WriteFile(baseFile, data, 0644); err != nil {
		t.Fatalf("write base spec: %v", err)
	}
	loader, err := newTestBundleLoader(t, baseFile, t.TempDir())
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	_, generated, err := loader.Generate(LoadOptions{
		ContainerID: "test-raise-process-baseline",
		Request: &apipb.CreateContainerRequest{
			Rootfs: &apipb.Rootfs{RootDir: t.TempDir()},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	assertHasCapability(t, generated.Process.Capabilities, "CAP_DAC_OVERRIDE")
	assertHasCapability(t, generated.Process.Capabilities, "CAP_SETUID")
	assertMissingCapability(t, &spec.LinuxCapabilities{Effective: generated.Process.Capabilities.Ambient}, "CAP_SETUID")
	for _, limit := range generated.Process.Rlimits {
		if limit.Type == "RLIMIT_NOFILE" {
			if limit.Soft != 1048576 || limit.Hard != 1048576 {
				t.Fatalf("RLIMIT_NOFILE = soft:%d hard:%d, want raised 1048576/1048576", limit.Soft, limit.Hard)
			}
			return
		}
	}
	t.Fatal("RLIMIT_NOFILE missing")
}

func TestSpecBuilderUsesExecutionProfile(t *testing.T) {
	base := defaultBundleSpec()
	base.Process.Args = []string{"/custom"}
	base.Process.Rlimits = []spec.POSIXRlimit{{Type: "RLIMIT_NOFILE", Soft: 1024, Hard: 1024}}
	builder := newSpecBuilder(ExecutionProfile{
		RuntimeBaseline: RuntimeBaselinePolicy{
			Capabilities: []string{"CAP_SYS_PTRACE"},
			NoFileLimit:  4096,
		},
		Capabilities: CapabilityPolicy{
			AnnotationKey:  "custom-capabilities",
			IncludeAmbient: false,
		},
		NetworkNamespace: DefaultNetworkNamespacePolicy(),
		Resources:        DefaultResourcePolicy(),
	})

	generated, err := builder.build(base, buildOptions{
		request: &apipb.CreateContainerRequest{
			Rootfs: &apipb.Rootfs{RootDir: t.TempDir()},
			Labels: map[string]string{
				"custom-capabilities": "CAP_SYS_ADMIN",
			},
		},
	})
	if err != nil {
		t.Fatalf("build() error = %v", err)
	}
	assertHasCapability(t, generated.Process.Capabilities, "CAP_SYS_PTRACE")
	assertHasCapability(t, generated.Process.Capabilities, "CAP_SYS_ADMIN")
	assertMissingCapabilityValue(t, generated.Process.Capabilities.Ambient, "CAP_SYS_ADMIN")
	for _, limit := range generated.Process.Rlimits {
		if limit.Type == "RLIMIT_NOFILE" {
			if limit.Soft != 4096 || limit.Hard != 4096 {
				t.Fatalf("RLIMIT_NOFILE = soft:%d hard:%d, want 4096/4096", limit.Soft, limit.Hard)
			}
			return
		}
	}
	t.Fatal("RLIMIT_NOFILE missing")
}

func TestBundleLoaderUsesExecutionProfileOption(t *testing.T) {
	loader, err := newTestBundleLoader(t, "", t.TempDir(), WithExecutionProfile(ExecutionProfile{
		RuntimeBaseline: RuntimeBaselinePolicy{
			Capabilities: []string{"CAP_SYS_PTRACE"},
			NoFileLimit:  2097152,
		},
	}))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	_, generated, err := loader.Generate(LoadOptions{
		ContainerID: "test-profile-option",
		Request: &apipb.CreateContainerRequest{
			Command: []string{"/bin/true"},
			Rootfs:  &apipb.Rootfs{RootDir: t.TempDir()},
			Labels: map[string]string{
				linuxCapabilitiesAnnoKey: "CAP_SYS_ADMIN",
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	assertHasCapability(t, generated.Process.Capabilities, "CAP_SYS_PTRACE")
	assertHasCapability(t, generated.Process.Capabilities, "CAP_SYS_ADMIN")
	assertMissingCapabilityValue(t, generated.Process.Capabilities.Ambient, "CAP_SYS_PTRACE")
	assertHasCapabilityValue(t, generated.Process.Capabilities.Ambient, "CAP_SYS_ADMIN")
	for _, limit := range generated.Process.Rlimits {
		if limit.Type == "RLIMIT_NOFILE" {
			if limit.Soft != 2097152 || limit.Hard != 2097152 {
				t.Fatalf("RLIMIT_NOFILE = soft:%d hard:%d, want profile 2097152/2097152", limit.Soft, limit.Hard)
			}
			return
		}
	}
	t.Fatal("RLIMIT_NOFILE missing")
}

func TestPrepareAndMaterializeBundleTemplateAvoidsDynamicLeakage(t *testing.T) {
	loader, err := newTestBundleLoader(t, "", t.TempDir())
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	rootfsDir := t.TempDir()
	template, err := loader.PrepareBundleTemplate(TemplateOptions{
		Request: &apipb.CreateContainerRequest{
			Command: []string{"/bin/sh", "-c", "sleep 300"},
			Rootfs:  &apipb.Rootfs{RootDir: rootfsDir, Readonly: true},
			Cwd:     "/workspace",
			Envs: []*runtimeapi.KeyValue{
				{Key: "STATIC_ENV", Value: "static"},
			},
			Mounts: []*runtimeapi.Mount{
				{Target: "/static", Type: "bind", Source: "/host/static", Options: []string{"ro"}},
			},
			Labels: map[string]string{
				workloadidentity.LabelKeyRuntimeID: "template-test",
			},
		},
	})
	if err != nil {
		t.Fatalf("PrepareBundleTemplate() error = %v", err)
	}

	firstBundleDir, firstSpec, err := loader.MaterializeBundle(template, LoadOptions{
		ContainerID: "bundle-first",
		Request: &apipb.CreateContainerRequest{
			Command: []string{"/bin/sh", "-c", "sleep 300"},
			Rootfs:  &apipb.Rootfs{RootDir: rootfsDir, Readonly: true},
			Cwd:     "/workspace",
			Envs: []*runtimeapi.KeyValue{
				{Key: "STATIC_ENV", Value: "static"},
				{Key: "USER_ENV", Value: "first"},
			},
			Mounts: []*runtimeapi.Mount{
				{Target: "/static", Type: "bind", Source: "/host/static", Options: []string{"ro"}},
				{Target: "/dynamic-first", Type: "bind", Source: "/host/first", Options: []string{"rw"}},
			},
			Labels: map[string]string{
				workloadidentity.LabelKeyRuntimeID:    "template-test",
				workloadidentity.LabelKeyServiceID:    "Claude_Code.Profile",
				workloadidentity.LabelKeyAllocationID: "alloc-FIRST-1234567890",
				"netac-rules":                         "10.0.0.0/24",
			},
		},
		CgroupPath:            "/sandbox/test/first",
		AdditionalAnnotations: map[string]string{"extra-annotation": "first"},
	})
	if err != nil {
		t.Fatalf("MaterializeBundle(first) error = %v", err)
	}
	if firstBundleDir == "" {
		t.Fatal("MaterializeBundle(first) returned empty bundle dir")
	}

	secondBundleDir, secondSpec, err := loader.MaterializeBundle(template, LoadOptions{
		ContainerID: "bundle-second",
		Request: &apipb.CreateContainerRequest{
			Command: []string{"/bin/sh", "-c", "sleep 300"},
			Rootfs:  &apipb.Rootfs{RootDir: rootfsDir, Readonly: true},
			Cwd:     "/workspace",
			Envs: []*runtimeapi.KeyValue{
				{Key: "STATIC_ENV", Value: "static"},
				{Key: "USER_ENV", Value: "second"},
			},
			Mounts: []*runtimeapi.Mount{
				{Target: "/static", Type: "bind", Source: "/host/static", Options: []string{"ro"}},
				{Target: "/dynamic-second", Type: "bind", Source: "/host/second", Options: []string{"rw"}},
			},
			Labels: map[string]string{
				workloadidentity.LabelKeyRuntimeID:    "template-test",
				workloadidentity.LabelKeyServiceID:    "Claude_Code.Profile",
				workloadidentity.LabelKeyAllocationID: "alloc-second-repeated-value",
				"netac-rules":                         "0.0.0.0/0",
			},
		},
		CgroupPath:            "/sandbox/test/second",
		AdditionalAnnotations: map[string]string{"extra-annotation": "second"},
	})
	if err != nil {
		t.Fatalf("MaterializeBundle(second) error = %v", err)
	}
	if secondBundleDir == firstBundleDir {
		t.Fatalf("bundle dirs should differ, both = %q", firstBundleDir)
	}

	assertEnvValue(t, firstSpec.Process.Env, "STATIC_ENV", "static")
	assertEnvValue(t, firstSpec.Process.Env, "USER_ENV", "first")
	assertEnvValue(t, secondSpec.Process.Env, "STATIC_ENV", "static")
	assertEnvValue(t, secondSpec.Process.Env, "USER_ENV", "second")
	assertEnvAbsent(t, secondSpec.Process.Env, "USER_ENV=first")

	assertMountPresent(t, firstSpec.Mounts, "/static", "/host/static")
	assertMountPresent(t, firstSpec.Mounts, "/dynamic-first", "/host/first")
	assertMountAbsent(t, firstSpec.Mounts, "/dynamic-second")
	assertMountPresent(t, secondSpec.Mounts, "/static", "/host/static")
	assertMountPresent(t, secondSpec.Mounts, "/dynamic-second", "/host/second")
	assertMountAbsent(t, secondSpec.Mounts, "/dynamic-first")

	if got := firstSpec.Linux.CgroupsPath; got != "/sandbox/test/first" {
		t.Fatalf("first cgroupsPath = %q, want /sandbox/test/first", got)
	}
	if got := secondSpec.Linux.CgroupsPath; got != "/sandbox/test/second" {
		t.Fatalf("second cgroupsPath = %q, want /sandbox/test/second", got)
	}
	if got := secondSpec.Annotations["extra-annotation"]; got != "second" {
		t.Fatalf("second extra annotation = %q, want second", got)
	}
	if got := firstSpec.Hostname; got != "claude-code-profile-alloc-first" {
		t.Fatalf("first hostname = %q, want claude-code-profile-alloc-first", got)
	}
	if got := secondSpec.Hostname; got != "claude-code-profile-alloc-second" {
		t.Fatalf("second hostname = %q, want claude-code-profile-alloc-second", got)
	}
	if got := secondSpec.Annotations[workloadidentity.LabelKeyHostname]; got != secondSpec.Hostname {
		t.Fatalf("second hostname annotation = %q, want %q", got, secondSpec.Hostname)
	}

	firstOnDisk, err := LoadSpec(filepath.Join(firstBundleDir, "config.json"))
	if err != nil {
		t.Fatalf("LoadSpec(first) error = %v", err)
	}
	secondOnDisk, err := LoadSpec(filepath.Join(secondBundleDir, "config.json"))
	if err != nil {
		t.Fatalf("LoadSpec(second) error = %v", err)
	}
	if !reflect.DeepEqual(firstOnDisk.Process.Env, firstSpec.Process.Env) {
		t.Fatalf("first on-disk env = %v, want %v", firstOnDisk.Process.Env, firstSpec.Process.Env)
	}
	if !reflect.DeepEqual(secondOnDisk.Process.Env, secondSpec.Process.Env) {
		t.Fatalf("second on-disk env = %v, want %v", secondOnDisk.Process.Env, secondSpec.Process.Env)
	}
	if firstOnDisk.Hostname != firstSpec.Hostname || secondOnDisk.Hostname != secondSpec.Hostname {
		t.Fatalf("on-disk hostnames = %q/%q, want %q/%q", firstOnDisk.Hostname, secondOnDisk.Hostname, firstSpec.Hostname, secondSpec.Hostname)
	}
}

func TestGenerateSetsWorkloadHostnameAndRuntimeEtcFiles(t *testing.T) {
	loader, err := newTestBundleLoader(t, "", t.TempDir())
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	bundleDir, generated, err := loader.Generate(LoadOptions{
		ContainerID: "alloc-ABCDEF1234567890",
		Request: &apipb.CreateContainerRequest{
			Command: []string{"/bin/true"},
			Rootfs:  &apipb.Rootfs{RootDir: t.TempDir()},
			Labels: map[string]string{
				workloadidentity.LabelKeyServiceID: "Claude_Code.Profile",
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got := generated.Hostname; got != "claude-code-profile-alloc-abcdef" {
		t.Fatalf("hostname = %q, want claude-code-profile-alloc-abcdef", got)
	}
	if got := generated.Annotations[workloadidentity.LabelKeyHostname]; got != generated.Hostname {
		t.Fatalf("hostname annotation = %q, want %q", got, generated.Hostname)
	}
	hostnameFile, err := os.ReadFile(filepath.Join(bundleDir, "sandbox-files", "hostname"))
	if err != nil {
		t.Fatalf("read managed hostname: %v", err)
	}
	if got := string(hostnameFile); got != generated.Hostname+"\n" {
		t.Fatalf("/etc/hostname = %q, want %q", got, generated.Hostname+"\n")
	}
	hostsFile, err := os.ReadFile(filepath.Join(bundleDir, "sandbox-files", "hosts"))
	if err != nil {
		t.Fatalf("read managed hosts: %v", err)
	}
	if !strings.Contains(string(hostsFile), "10.88.0.2 "+generated.Hostname+"\n") {
		t.Fatalf("/etc/hosts = %q, want workload hostname entry", string(hostsFile))
	}
}

func TestGenerateCompactsOpaqueServiceIDHostname(t *testing.T) {
	loader, err := newTestBundleLoader(t, "", t.TempDir())
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	_, generated, err := loader.Generate(LoadOptions{
		ContainerID: "alloc-aaaaaaaaaaaa",
		Request: &apipb.CreateContainerRequest{
			Command: []string{"/bin/true"},
			Rootfs:  &apipb.Rootfs{RootDir: t.TempDir()},
			Labels: map[string]string{
				workloadidentity.LabelKeyServiceID: "svc-bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb",
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if got := generated.Hostname; got != "svc-bbbbbbbb-alloc-aaaaaa" {
		t.Fatalf("hostname = %q, want svc-bbbbbbbb-alloc-aaaaaa", got)
	}
}

func TestMaterializeBundleAddsManagedEtcFileMounts(t *testing.T) {
	loader, err := newTestBundleLoader(t, "", t.TempDir(), WithRuntimeDNSConfig(RuntimeDNSConfig{
		Nameservers: []string{"10.0.0.2"},
	}))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}
	bundleDir, generated, err := loader.Generate(LoadOptions{
		ContainerID: "test-managed-etc",
		Request: &apipb.CreateContainerRequest{
			Command: []string{"/bin/true"},
			Rootfs:  &apipb.Rootfs{RootDir: t.TempDir()},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	for _, name := range []string{"hostname", "hosts", "resolv.conf"} {
		path := filepath.Join(bundleDir, "sandbox-files", name)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read managed %s: %v", name, err)
		}
		if len(content) == 0 {
			t.Fatalf("managed %s is empty", name)
		}
		assertMountPresent(t, generated.Mounts, "/etc/"+name, path)
	}
}

func TestMaterializeBundleUsesIndividualFilesWhenRootfsEtcFileIsMissing(t *testing.T) {
	loader, err := newTestBundleLoader(t, "", t.TempDir(), WithRuntimeDNSConfig(RuntimeDNSConfig{
		Nameservers: []string{"10.0.0.2"},
	}))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}
	rootfsDir := t.TempDir()
	for _, name := range []string{"hostname", "resolv.conf", "os-release"} {
		path := filepath.Join(rootfsDir, "etc", name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("mkdir rootfs etc: %v", err)
		}
		if err := os.WriteFile(path, []byte("image-"+name+"\n"), 0644); err != nil {
			t.Fatalf("write rootfs %s: %v", name, err)
		}
	}

	bundleDir, generated, err := loader.Generate(LoadOptions{
		ContainerID: "test-managed-etc-dir",
		Request: &apipb.CreateContainerRequest{
			Command: []string{"/bin/true"},
			Rootfs:  &apipb.Rootfs{RootDir: rootfsDir, Readonly: true},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	managedFilesDir := filepath.Join(bundleDir, "sandbox-files")
	assertMountAbsent(t, generated.Mounts, "/etc")
	for _, name := range []string{"hostname", "hosts", "resolv.conf"} {
		content, err := os.ReadFile(filepath.Join(managedFilesDir, name))
		if err != nil {
			t.Fatalf("read sandbox file %s: %v", name, err)
		}
		if len(content) == 0 {
			t.Fatalf("sandbox file %s is empty", name)
		}
		assertMountPresent(t, generated.Mounts, "/etc/"+name, filepath.Join(managedFilesDir, name))
	}
	if content, err := os.ReadFile(filepath.Join(rootfsDir, "etc", "os-release")); err != nil || string(content) != "image-os-release\n" {
		t.Fatalf("lower os-release changed: %q err=%v", content, err)
	}
}

func TestMaterializeBundleDoesNotCopyRootfsEtcSymlinks(t *testing.T) {
	loader, err := newTestBundleLoader(t, "", t.TempDir(), WithRuntimeDNSConfig(RuntimeDNSConfig{
		Nameservers: []string{"10.0.0.2"},
	}))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}
	rootfsDir := t.TempDir()
	etcDir := filepath.Join(rootfsDir, "etc")
	if err := os.MkdirAll(etcDir, 0755); err != nil {
		t.Fatalf("mkdir rootfs etc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "hostname"), []byte("image-hostname\n"), 0644); err != nil {
		t.Fatalf("write rootfs hostname: %v", err)
	}
	if err := os.WriteFile(filepath.Join(etcDir, "shadow-hosts"), []byte("image-shadow-hosts\n"), 0644); err != nil {
		t.Fatalf("write rootfs shadow-hosts: %v", err)
	}
	if err := os.Symlink("shadow-hosts", filepath.Join(etcDir, "hosts")); err != nil {
		t.Fatalf("symlink rootfs hosts: %v", err)
	}

	bundleDir, _, err := loader.Generate(LoadOptions{
		ContainerID: "test-managed-etc-dir-symlink",
		Request: &apipb.CreateContainerRequest{
			Command: []string{"/bin/true"},
			Rootfs:  &apipb.Rootfs{RootDir: rootfsDir, Readonly: true},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	managedEtcDir := filepath.Join(bundleDir, "sandbox-files")
	hostsPath := filepath.Join(managedEtcDir, "hosts")
	hostsInfo, err := os.Lstat(hostsPath)
	if err != nil {
		t.Fatalf("lstat managed hosts: %v", err)
	}
	if hostsInfo.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("managed hosts is still a symlink")
	}
	hosts, err := os.ReadFile(hostsPath)
	if err != nil {
		t.Fatalf("read managed hosts: %v", err)
	}
	if !strings.Contains(string(hosts), "127.0.0.1 localhost") {
		t.Fatalf("managed hosts = %q, want runtime hosts content", hosts)
	}
	if _, err := os.Stat(filepath.Join(managedEtcDir, "shadow-hosts")); !os.IsNotExist(err) {
		t.Fatalf("rootfs shadow-hosts was copied into sandbox files: %v", err)
	}
	shadowHosts, err := os.ReadFile(filepath.Join(rootfsDir, "etc", "shadow-hosts"))
	if err != nil || string(shadowHosts) != "image-shadow-hosts\n" {
		t.Fatalf("lower shadow-hosts changed: %q err=%v", shadowHosts, err)
	}
}

func TestBuildResolvConfFromDockerEmbeddedDNSComments(t *testing.T) {
	content := `# Generated by Docker Engine.
nameserver 127.0.0.11
options ndots:0
# ExtServers: [host(0.250.250.200)]
`

	got, err := buildResolvConfFromContent(content, []string{"ndots:0"})
	if err != nil {
		t.Fatalf("buildResolvConfFromContent() error = %v", err)
	}
	if !strings.Contains(got, "nameserver 0.250.250.200\n") {
		t.Fatalf("resolv.conf = %q, want Docker external nameserver", got)
	}
	if strings.Contains(got, "127.0.0.11") {
		t.Fatalf("resolv.conf = %q, should not include loopback Docker resolver", got)
	}
}

func TestBuildResolvConfOwnsInertFileWithoutDerivedNameserver(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing-resolv.conf")
	got, err := buildResolvConf(RuntimeDNSConfig{HostResolvConfPaths: []string{missing}})
	if err != nil {
		t.Fatalf("buildResolvConf() error = %v", err)
	}
	if got != noNameserverResolvConf {
		t.Fatalf("buildResolvConf() = %q, want inert resolver file %q", got, noNameserverResolvConf)
	}

	loader, err := newTestBundleLoader(t, "", t.TempDir(), WithRuntimeDNSConfig(RuntimeDNSConfig{HostResolvConfPaths: []string{missing}}))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}
	bundleDir, generated, err := loader.Generate(LoadOptions{
		ContainerID: "resolver-independent",
		Request: &apipb.CreateContainerRequest{
			Command: []string{"/bin/true"},
			Rootfs:  &apipb.Rootfs{RootDir: t.TempDir()},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	resolvPath := filepath.Join(bundleDir, "sandbox-files", "resolv.conf")
	content, err := os.ReadFile(resolvPath)
	if err != nil || string(content) != noNameserverResolvConf {
		t.Fatalf("managed resolv.conf = %q, err=%v", content, err)
	}
	assertMountPresent(t, generated.Mounts, "/etc/resolv.conf", resolvPath)
}

func TestBuildResolvConfRejectsInvalidExplicitNameserver(t *testing.T) {
	_, err := buildResolvConf(RuntimeDNSConfig{Nameservers: []string{"127.0.0.1", "invalid"}})
	if !errors.Is(err, errNoUsableNameserver) {
		t.Fatalf("buildResolvConf() error = %v, want no usable nameserver", err)
	}
}

func TestResolveRuntimeDNSNameserversRemainsStrictWithoutDerivedNameserver(t *testing.T) {
	_, err := ResolveRuntimeDNSNameservers(RuntimeDNSConfig{
		HostResolvConfPaths: []string{filepath.Join(t.TempDir(), "missing-resolv.conf")},
	})
	if !errors.Is(err, errNoUsableNameserver) {
		t.Fatalf("ResolveRuntimeDNSNameservers() error = %v, want no usable nameserver", err)
	}
}

func TestMaterializeBundleHonorsExistingResolvConfMount(t *testing.T) {
	loader, err := newTestBundleLoader(t, "", t.TempDir(), WithRuntimeDNSConfig(RuntimeDNSConfig{
		HostResolvConfPaths: []string{filepath.Join(t.TempDir(), "missing-resolv.conf")},
	}))
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}
	rootfsDir := t.TempDir()
	sourceResolv := filepath.Join(t.TempDir(), "resolv.conf")

	_, generated, err := loader.Generate(LoadOptions{
		ContainerID: "test-existing-resolv-conf",
		Request: &apipb.CreateContainerRequest{
			Command: []string{"/bin/true"},
			Rootfs:  &apipb.Rootfs{RootDir: rootfsDir},
			Mounts: []*runtimeapi.Mount{
				{Target: "/etc/resolv.conf", Type: "bind", Source: sourceResolv, Options: []string{"ro"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	assertMountPresent(t, generated.Mounts, "/etc/resolv.conf", sourceResolv)
}

func TestBuildResolvConfUsesExplicitDNSConfig(t *testing.T) {
	got, err := buildResolvConf(RuntimeDNSConfig{
		Nameservers:   []string{"10.0.0.2", "127.0.0.1"},
		SearchDomains: []string{"svc.cluster.local", "cluster.local"},
		Options:       []string{"ndots:5", "timeout:2"},
	})
	if err != nil {
		t.Fatalf("buildResolvConf() error = %v", err)
	}
	want := "nameserver 10.0.0.2\nsearch svc.cluster.local cluster.local\noptions ndots:5 timeout:2\n"
	if got != want {
		t.Fatalf("resolv.conf = %q, want %q", got, want)
	}
}

func TestDefaultBundleSpecUsesGenericRuntimeDefaults(t *testing.T) {
	spec := defaultBundleSpec()
	if spec == nil || spec.Process == nil {
		t.Fatal("defaultBundleSpec() returned nil process")
	}
	if got := spec.Process.Cwd; got != "/" {
		t.Fatalf("default cwd = %q, want /", got)
	}
	if hasEnv(spec.Process.Env, "LD_LIBRARY_PATH") {
		t.Fatalf("default env unexpectedly contains LD_LIBRARY_PATH: %v", spec.Process.Env)
	}
	if !hasEnvValue(spec.Process.Env, "PATH", "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin") {
		t.Fatalf("default PATH missing generic runtime value: %v", spec.Process.Env)
	}
	if !hasMount(spec.Mounts, "/dev/pts") {
		t.Fatalf("default mounts missing /dev/pts for tty exec support: %v", spec.Mounts)
	}
	assertHasCapability(t, spec.Process.Capabilities, "CAP_DAC_OVERRIDE")
	assertHasCapability(t, spec.Process.Capabilities, "CAP_SETUID")
	assertMissingCapabilityValue(t, spec.Process.Capabilities.Ambient, "CAP_SETUID")
	for _, limit := range spec.Process.Rlimits {
		if limit.Type == "RLIMIT_NOFILE" {
			if limit.Soft != 1048576 || limit.Hard != 1048576 {
				t.Fatalf("default RLIMIT_NOFILE = soft:%d hard:%d, want 1048576/1048576", limit.Soft, limit.Hard)
			}
			return
		}
	}
	t.Fatal("default RLIMIT_NOFILE missing")
}

func assertHasCapability(t *testing.T, capabilities *spec.LinuxCapabilities, name string) {
	t.Helper()
	if capabilities == nil {
		t.Fatal("capabilities = nil")
	}
	for _, value := range capabilities.Effective {
		if value == name {
			return
		}
	}
	t.Fatalf("capability %q missing from effective set %v", name, capabilities.Effective)
}

func assertMissingCapability(t *testing.T, capabilities *spec.LinuxCapabilities, name string) {
	t.Helper()
	if capabilities == nil {
		return
	}
	for _, value := range capabilities.Effective {
		if value == name {
			t.Fatalf("capability %q unexpectedly present in effective set %v", name, capabilities.Effective)
		}
	}
}

func assertMissingCapabilityValue(t *testing.T, values []string, name string) {
	t.Helper()
	for _, value := range values {
		if value == name {
			t.Fatalf("capability %q unexpectedly present in %v", name, values)
		}
	}
}

func assertHasCapabilityValue(t *testing.T, values []string, name string) {
	t.Helper()
	for _, value := range values {
		if value == name {
			return
		}
	}
	t.Fatalf("capability %q missing from %v", name, values)
}

func assertEnvValue(t *testing.T, envs []string, key, wantValue string) {
	t.Helper()
	for _, env := range envs {
		if env == key+"="+wantValue {
			return
		}
	}
	t.Fatalf("env %q missing from %v", key+"="+wantValue, envs)
}

func assertEnvAbsent(t *testing.T, envs []string, unwanted string) {
	t.Helper()
	for _, env := range envs {
		if env == unwanted {
			t.Fatalf("unexpected env %q present in %v", unwanted, envs)
		}
	}
}

func assertMountPresent(t *testing.T, mounts []spec.Mount, target, source string) {
	t.Helper()
	for _, mnt := range mounts {
		if mnt.Destination == target && mnt.Source == source {
			return
		}
	}
	t.Fatalf("mount %s <- %s missing from %v", target, source, mounts)
}

func assertMountAbsent(t *testing.T, mounts []spec.Mount, target string) {
	t.Helper()
	for _, mnt := range mounts {
		if mnt.Destination == target {
			t.Fatalf("unexpected mount %s present in %v", target, mounts)
		}
	}
}

func hasEnv(envs []string, key string) bool {
	for _, env := range envs {
		if strings.HasPrefix(env, key+"=") {
			return true
		}
	}
	return false
}

func hasEnvValue(envs []string, key, want string) bool {
	for _, env := range envs {
		if env == key+"="+want {
			return true
		}
	}
	return false
}

func hasMount(mounts []spec.Mount, target string) bool {
	for _, mnt := range mounts {
		if mnt.Destination == target {
			return true
		}
	}
	return false
}
