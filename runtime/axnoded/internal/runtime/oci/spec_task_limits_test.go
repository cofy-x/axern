package oci

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func TestTaskLimitProfileRejectsInvalidPairsBeforeFilesystemChanges(t *testing.T) {
	for _, pair := range [][2]int64{{-1, 64}, {64, -1}, {0, 64}, {64, 0}, {63, 64}, {64, 63}, {65537, 64}, {64, 65537}} {
		profile := ExecutionProfile{GuestTaskLimit: pair[0], HostTaskLimit: pair[1]}
		dir := filepath.Join(t.TempDir(), "bundles")
		if _, err := NewBundleLoader(dir, WithExecutionProfile(profile)); err == nil {
			t.Fatalf("NewBundleLoader(%v) accepted invalid limits", pair)
		}
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("invalid profile created bundle directory: %v", err)
		}
		loader, err := NewBundleLoader(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := loader.PrepareBundleTemplate(TemplateOptions{
			Request: &apipb.CreateContainerRequest{Command: []string{"/bin/true"}}, ExecutionProfile: &profile,
		}); err == nil {
			t.Fatalf("PrepareBundleTemplate(%v) accepted invalid limits", pair)
		}
	}
	for _, limit := range []int64{64, 65536} {
		if _, err := NewBundleLoader(t.TempDir(), WithExecutionProfile(ExecutionProfile{GuestTaskLimit: limit, HostTaskLimit: limit})); err != nil {
			t.Fatalf("valid boundary %d: %v", limit, err)
		}
	}
}

func TestTaskLimitPolicyRestoresPreparedTemplateBoundary(t *testing.T) {
	profile := ExecutionProfile{GuestTaskLimit: 128, HostTaskLimit: 512}
	loader, err := NewBundleLoader(t.TempDir(), WithExecutionProfile(profile))
	if err != nil {
		t.Fatal(err)
	}
	request := &apipb.CreateContainerRequest{Command: []string{"/bin/true"}, Rootfs: &apipb.Rootfs{RootDir: t.TempDir()}}
	for _, mode := range []string{"removed", "overridden"} {
		t.Run(mode, func(t *testing.T) {
			template, err := loader.PrepareBundleTemplate(TemplateOptions{Request: request})
			if err != nil {
				t.Fatal(err)
			}
			if mode == "removed" {
				template.spec.Linux = nil
				template.spec.Mounts = nil
			} else {
				unlimited := int64(-1)
				template.spec.Linux.Resources.Pids.Limit = &unlimited
				template.spec.Linux.Namespaces = append(template.spec.Linux.Namespaces,
					spec.LinuxNamespace{Type: spec.CgroupNamespace, Path: "/proc/1/ns/cgroup"})
				template.spec.Mounts = append(template.spec.Mounts,
					spec.Mount{Destination: "/sys", Type: "bind", Source: "/host/sys"},
					spec.Mount{Destination: "/sys/fs/./cgroup", Type: "bind", Source: "/host/cgroup", Options: []string{"rw"}})
			}
			_, generated, err := loader.MaterializeBundle(template, LoadOptions{
				ContainerID: mode, SandboxdInjection: taskLimitTestInjection(t),
				Request: &apipb.CreateContainerRequest{Resource: &apipb.LinuxContainerResources{Unified: map[string]string{"pids.max": "max"}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			if generated.Linux.Resources.Pids.Limit == nil || *generated.Linux.Resources.Pids.Limit != 512 {
				t.Fatalf("host task limit = %v", generated.Linux.Resources.Pids.Limit)
			}
			if _, ok := generated.Linux.Resources.Unified["pids.max"]; ok {
				t.Fatal("unified pids.max can override the trusted host limit")
			}
			var namespaces []spec.LinuxNamespace
			for _, ns := range generated.Linux.Namespaces {
				if ns.Type == spec.CgroupNamespace {
					namespaces = append(namespaces, ns)
				}
			}
			if !reflect.DeepEqual(namespaces, []spec.LinuxNamespace{{Type: spec.CgroupNamespace}}) {
				t.Fatalf("cgroup namespaces = %+v", namespaces)
			}
			var mounts []spec.Mount
			cgroupMountIndex := -1
			for index, mount := range generated.Mounts {
				if mount.Destination == guestCgroupMountPath {
					mounts = append(mounts, mount)
					cgroupMountIndex = index
				}
			}
			wantMounts := []spec.Mount{{Destination: guestCgroupMountPath, Type: "cgroup2", Source: "cgroup2", Options: []string{"ro", "nosuid", "noexec", "nodev"}}}
			if !reflect.DeepEqual(mounts, wantMounts) {
				t.Fatalf("cgroup mounts = %+v", mounts)
			}
			for index, mount := range generated.Mounts {
				if index > cgroupMountIndex && mountDestinationOwns(mount.Destination, guestCgroupMountPath) {
					t.Fatalf("later mount %s can shadow the cgroup mount", mount.Destination)
				}
			}
			if !reflect.DeepEqual(generated.Process.Capabilities, defaultBundleSpec().Process.Capabilities) {
				t.Fatal("task policy changed baseline capabilities")
			}
		})
	}
}

func TestTaskLimitPolicyRejectsNestedMountsAndSysAdmin(t *testing.T) {
	profile := ExecutionProfile{GuestTaskLimit: 64, HostTaskLimit: 64}
	for _, destination := range []string{"/sys/fs/cgroup/pids.max", "/sys/fs/cgroup/child", "/sys/fs/cgroup/../cgroup/pids.max"} {
		base := defaultBundleSpec()
		base.Process.Args = []string{"/bin/true"}
		base.Mounts = append(base.Mounts, spec.Mount{Destination: destination, Type: "bind", Source: "/host"})
		if _, err := newSpecBuilder(profile).build(base, buildOptions{}); err == nil {
			t.Fatalf("accepted mount at %s", destination)
		}
	}
	for _, set := range []string{"bounding", "effective", "permitted", "inheritable", "ambient"} {
		base := defaultBundleSpec()
		base.Process.Args = []string{"/bin/true"}
		caps := base.Process.Capabilities
		target := map[string]*[]string{"bounding": &caps.Bounding, "effective": &caps.Effective, "permitted": &caps.Permitted, "inheritable": &caps.Inheritable, "ambient": &caps.Ambient}[set]
		*target = append(*target, "CAP_SYS_ADMIN")
		if _, err := newSpecBuilder(profile).build(base, buildOptions{}); err == nil {
			t.Fatalf("accepted CAP_SYS_ADMIN in %s", set)
		}
	}
}

func TestTaskLimitInjectionSeparatesTrustedLimitFromUserArgs(t *testing.T) {
	loader, err := NewBundleLoader(t.TempDir(), WithExecutionProfile(ExecutionProfile{GuestTaskLimit: 64, HostTaskLimit: 256}))
	if err != nil {
		t.Fatal(err)
	}
	userArgs := []string{"/bin/echo", "--guest-task-limit", "0", "--guest-task-limit=65536"}
	bundleDir, generated, err := loader.Generate(LoadOptions{
		ContainerID: "trusted-limit", SandboxdInjection: taskLimitTestInjection(t),
		Request: &apipb.CreateContainerRequest{Command: userArgs, Rootfs: &apipb.Rootfs{RootDir: t.TempDir()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantArgs := []string{SandboxdGuestBinaryPath, "--socket", SandboxdGuestSocketPath, "--entrypoint-json", SandboxdGuestEntrypointPath, "--guest-task-limit", "64"}
	if !slices.Equal(generated.Process.Args, wantArgs) {
		t.Fatalf("daemon args = %q", generated.Process.Args)
	}
	data, err := os.ReadFile(filepath.Join(bundleDir, sandboxdBundleMetadataDir, sandboxdBundleEntrypointName))
	if err != nil {
		t.Fatal(err)
	}
	var entrypoint sandboxdEntrypointMetadata
	if err := json.Unmarshal(data, &entrypoint); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(entrypoint.Args, userArgs) {
		t.Fatalf("user args = %q", entrypoint.Args)
	}
}

func TestTaskLimitMaterializationRejectsMissingInjectionOrChangedPolicy(t *testing.T) {
	profile := ExecutionProfile{GuestTaskLimit: 64, HostTaskLimit: 256}
	for _, mode := range []string{"missing-injection", "changed-profile", "changed-loader-digest"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			loader, err := NewBundleLoader(root, WithExecutionProfile(profile))
			if err != nil {
				t.Fatal(err)
			}
			template, err := loader.PrepareBundleTemplate(TemplateOptions{Request: &apipb.CreateContainerRequest{Command: []string{"/bin/true"}}})
			if err != nil {
				t.Fatal(err)
			}
			opts := LoadOptions{ContainerID: "rejected"}
			if mode != "missing-injection" {
				opts.SandboxdInjection = taskLimitTestInjection(t)
			}
			if mode == "changed-profile" {
				disabled := DefaultExecutionProfile()
				opts.ExecutionProfile = &disabled
			}
			if mode == "changed-loader-digest" {
				loader.runtimeFiles.DNS.Nameservers = []string{"192.0.2.1"}
			}
			if _, _, err := loader.MaterializeBundle(template, opts); err == nil {
				t.Fatalf("accepted %s", mode)
			}
			if _, err := os.Stat(filepath.Join(root, opts.ContainerID)); !os.IsNotExist(err) {
				t.Fatalf("rejected materialization created files: %v", err)
			}
		})
	}
}

func TestDisabledTaskLimitsPreserveBaseline(t *testing.T) {
	base := defaultBundleSpec()
	base.Process.Args = []string{"/bin/true"}
	want := deepCopySpec(base)
	want.Hostname = "sandbox"
	generated, err := newSpecBuilder(DefaultExecutionProfile()).build(base, buildOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(generated, want) {
		t.Fatal("disabled limits changed baseline OCI spec")
	}
	if err := materializeSandboxdInjection(t.TempDir(), generated, taskLimitTestInjection(t), 0); err != nil {
		t.Fatal(err)
	}
	for _, arg := range generated.Process.Args {
		if strings.HasPrefix(arg, "--guest-task-limit") {
			t.Fatalf("disabled task limits injected %q", arg)
		}
	}
}

func TestDisabledTaskLimitsPreserveTemplateProfileInheritanceAndOverrides(t *testing.T) {
	for _, explicitOverride := range []bool{false, true} {
		loader, err := NewBundleLoader(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		profile := DefaultExecutionProfile()
		profile.Baseline.NoFileLimit = 2097152
		profile.Baseline.Capabilities = []string{"CAP_SYS_PTRACE"}
		template, err := loader.PrepareBundleTemplate(TemplateOptions{
			Request:          &apipb.CreateContainerRequest{Command: []string{"/bin/true"}, Rootfs: &apipb.Rootfs{RootDir: t.TempDir()}},
			ExecutionProfile: &profile,
		})
		if err != nil {
			t.Fatal(err)
		}
		// Disabled task limits retain the existing loader/template behavior,
		// including applying the current runtime files configuration.
		loader.runtimeFiles.DNS.Nameservers = []string{"192.0.2.1"}
		opts := LoadOptions{ContainerID: "baseline-template"}
		wantNoFileLimit := uint64(2097152)
		if explicitOverride {
			profile.Baseline.NoFileLimit = 3145728
			opts.ExecutionProfile = &profile
			wantNoFileLimit = 3145728
		}
		_, generated, err := loader.MaterializeBundle(template, opts)
		if err != nil {
			t.Fatalf("MaterializeBundle(explicitOverride=%t): %v", explicitOverride, err)
		}
		assertHasCapability(t, generated.Process.Capabilities, "CAP_SYS_PTRACE")
		found := false
		for _, limit := range generated.Process.Rlimits {
			if limit.Type == "RLIMIT_NOFILE" {
				found = true
				if limit.Soft != wantNoFileLimit || limit.Hard != wantNoFileLimit {
					t.Fatalf("RLIMIT_NOFILE = %+v, want %d", limit, wantNoFileLimit)
				}
			}
		}
		if !found {
			t.Fatal("missing inherited RLIMIT_NOFILE")
		}
	}
}

func TestTaskLimitLoaderRejectsPerTemplateLimitOverrides(t *testing.T) {
	loaderProfile := ExecutionProfile{GuestTaskLimit: 128, HostTaskLimit: 256}
	loader, err := NewBundleLoader(t.TempDir(), WithExecutionProfile(loaderProfile))
	if err != nil {
		t.Fatal(err)
	}
	request := &apipb.CreateContainerRequest{Command: []string{"/bin/true"}, Rootfs: &apipb.Rootfs{RootDir: t.TempDir()}}
	for _, pair := range [][2]int64{{0, 0}, {64, 256}, {128, 512}} {
		override := ExecutionProfile{GuestTaskLimit: pair[0], HostTaskLimit: pair[1]}
		if _, err := loader.PrepareBundleTemplate(TemplateOptions{Request: request, ExecutionProfile: &override}); err == nil {
			t.Fatalf("enabled loader accepted template task limits %v", pair)
		}
	}
	// Baseline customization is still valid when the node-owned limits match.
	customProfile := loaderProfile
	customProfile.Baseline.NoFileLimit = 2097152
	template, err := loader.PrepareBundleTemplate(TemplateOptions{Request: request, ExecutionProfile: &customProfile})
	if err != nil {
		t.Fatal(err)
	}
	_, generated, err := loader.MaterializeBundle(template, LoadOptions{ContainerID: "custom-baseline", SandboxdInjection: taskLimitTestInjection(t)})
	if err != nil {
		t.Fatalf("matching task limits with inherited custom baseline: %v", err)
	}
	if generated.Linux.Resources.Pids.Limit == nil || *generated.Linux.Resources.Pids.Limit != loaderProfile.HostTaskLimit {
		t.Fatal("inherited custom baseline changed the node-owned host task limit")
	}
}

func taskLimitTestInjection(t *testing.T) *SandboxdInjectionOptions {
	t.Helper()
	binaryPath := filepath.Join(t.TempDir(), "sandboxd")
	if err := os.WriteFile(binaryPath, staticSandboxdFixture(), 0700); err != nil {
		t.Fatal(err)
	}
	return &SandboxdInjectionOptions{HostBinaryPath: binaryPath}
}
