package environmentcache

import (
	"fmt"
	"testing"
	"time"

	api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"google.golang.org/protobuf/proto"
)

func TestGetPreparedEnvironment_NotFound(t *testing.T) {
	lm := NewEnvironmentCache(&mockMounter{})
	environment := lm.GetPreparedEnvironment("nonexistent")
	if environment != nil {
		t.Fatal("expected nil for nonexistent runtime")
	}
}

func TestAddAndGetPreparedEnvironment(t *testing.T) {
	lm := NewEnvironmentCache(&mockMounter{})

	fr := newTestFR("rt-1", "/some/path")
	environment, err := addTestEnvironmentCache(lm, fr)
	if err != nil {
		t.Fatalf("PrepareEnvironment failed: %v", err)
	}
	if environment.ID != "rt-1" {
		t.Fatalf("expected ID rt-1, got %s", environment.ID)
	}

	got := lm.GetPreparedEnvironment("rt-1")
	if got == nil {
		t.Fatal("expected to find runtime rt-1")
	}
	if got != environment {
		t.Fatal("expected same pointer")
	}
}

func TestPrepareEnvironment_Duplicate(t *testing.T) {
	lm := NewEnvironmentCache(&mockMounter{})

	fr := newTestFR("rt-1", "/some/path")
	lr1, err := addTestEnvironmentCache(lm, fr)
	if err != nil {
		t.Fatalf("first PrepareEnvironment failed: %v", err)
	}

	lr2, err := addTestEnvironmentCache(lm, fr)
	if err != nil {
		t.Fatalf("second PrepareEnvironment failed: %v", err)
	}
	if lr1 != lr2 {
		t.Fatal("expected same runtime for duplicate add")
	}
}

func TestPrepareEnvironment_DriftedIdleEnvironmentReplaced(t *testing.T) {
	mock := &mockMounter{}
	lm := NewEnvironmentCache(mock)
	lm.ConfigureRetention(time.Minute, 8)

	fr := newTestFR("rt-drift", "/some/path")
	fr.Cwd = "/workspace-a"
	fr.Mounts = []*api.Mount{{Type: "bind", Source: "/host/a", Target: "/data", Options: []string{"ro"}}}
	lr1, err := addTestEnvironmentCache(lm, fr)
	if err != nil {
		t.Fatalf("PrepareEnvironment(first) failed: %v", err)
	}

	loader, err := runtimeoci.NewBundleLoader("", t.TempDir())
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}
	_, _, err = lr1.LoadOrPrepareBundleTemplate(func() (*runtimeoci.BundleTemplate, error) {
		return loader.PrepareBundleTemplate(runtimeoci.TemplateOptions{
			Request: &apipb.CreateContainerRequest{
				Command: []string{"/bin/sh"},
				Rootfs:  &apipb.Rootfs{RootDir: t.TempDir(), Readonly: true},
			},
		})
	})
	if err != nil {
		t.Fatalf("LoadOrPrepareBundleTemplate() error = %v", err)
	}

	lr1.IncRef()
	lr1.DecRef()
	if !lr1.Retained() {
		t.Fatal("expected runtime to be retained before drifted replacement")
	}
	drifted := newTestFR("rt-drift", "/other/path")
	drifted.Argv = []string{"/bin/bash"}
	drifted.Cwd = "/workspace-b"
	drifted.Env = map[string]string{"FOO": "bar"}
	drifted.Mounts = []*api.Mount{{Type: "bind", Source: "/host/b", Target: "/data", Options: []string{"rw"}}}
	lr2, created, err := addTestEnvironmentCacheWithState(lm, drifted)
	if err != nil {
		t.Fatalf("PrepareEnvironment(drifted) failed: %v", err)
	}
	if !created {
		t.Fatal("expected drifted runtime to force replacement")
	}
	if lr2 == lr1 {
		t.Fatal("expected replacement runtime pointer")
	}
	if !lr1.Released() {
		t.Fatal("expected previous runtime to be released after replacement")
	}
	if lr1.template != nil {
		t.Fatal("expected previous bundle template to be cleared on replacement")
	}
	if got := lr2.RootFS.Config().Path; got != "/other/path" {
		t.Fatalf("replacement rootfs path = %q, want /other/path", got)
	}
	if got := lr2.Cwd; got != "/workspace-b" {
		t.Fatalf("replacement cwd = %q, want /workspace-b", got)
	}
	if got := lr2.Mounts[0].Source; got != "/host/b" {
		t.Fatalf("replacement mount source = %q, want /host/b", got)
	}
}

func TestPrepareEnvironment_DriftedActiveRuntimeSuperseded(t *testing.T) {
	lm := NewEnvironmentCache(&mockMounter{})

	environment, err := addTestEnvironmentCache(lm, newTestFR("rt-active-drift", "/some/path"))
	if err != nil {
		t.Fatalf("PrepareEnvironment() failed: %v", err)
	}
	environment.IncRef()

	drifted := newTestFR("rt-active-drift", "/other/path")
	drifted.Argv = []string{"/bin/bash"}
	replacement, created, err := addTestEnvironmentCacheWithState(lm, drifted)
	if err != nil {
		t.Fatalf("PrepareEnvironment(drifted) failed: %v", err)
	}
	if !created {
		t.Fatal("expected drifted active prepared environment to create replacement")
	}
	if replacement == environment {
		t.Fatal("expected replacement runtime pointer")
	}
	if current := lm.GetPreparedEnvironment("rt-active-drift"); current != replacement {
		t.Fatal("expected replacement runtime to be registered")
	}
	if environment.Released() {
		t.Fatal("expected active superseded runtime to stay alive until its last ref is released")
	}
	environment.DecRef()
	if !environment.Released() {
		t.Fatal("expected superseded runtime to be released after its last ref")
	}
	if current := lm.GetPreparedEnvironment("rt-active-drift"); current != replacement {
		t.Fatal("expected replacement runtime to remain registered after superseded runtime release")
	}
}

func TestPrepareEnvironment_ImageCacheKeyDriftSupersedesActiveRuntime(t *testing.T) {
	currentCacheKey := "example.local/agent:dev@sha256:111"
	mock := &mockMounter{
		resolveFunc: func(cfg RootfsConfig) (RootfsConfig, error) {
			cfg.ImageCacheKey = currentCacheKey
			return cfg, nil
		},
	}
	lm := NewEnvironmentCache(mock)
	fr := &api.ResolvedEnvironment{
		ID: "agent",
		Rootfs: &api.RootfsConfig{
			Type:   api.RootfsSrcType_IMAGE,
			Source: &api.RootfsConfig_ImageUrl{ImageUrl: "example.local/agent:dev"},
		},
		Argv: []string{"/bin/sh"},
	}

	lr1, err := addTestEnvironmentCache(lm, fr)
	if err != nil {
		t.Fatalf("PrepareEnvironment(first) failed: %v", err)
	}
	lr1.IncRef()

	currentCacheKey = "example.local/agent:dev@sha256:222"
	lr2, created, err := addTestEnvironmentCacheWithState(lm, fr)
	if err != nil {
		t.Fatalf("PrepareEnvironment(second) failed: %v", err)
	}
	if !created || lr2 == lr1 {
		t.Fatalf("replacement = (%p, created=%v), want new runtime", lr2, created)
	}
	if got := lr2.RootFS.Config().ImageCacheKey; got != currentCacheKey {
		t.Fatalf("replacement ImageCacheKey = %q, want %q", got, currentCacheKey)
	}
	if lr1.Released() {
		t.Fatal("expected active old runtime to stay alive")
	}
	lr1.DecRef()
	if !lr1.Released() {
		t.Fatal("expected old runtime to release after last ref")
	}
	if got := lm.GetPreparedEnvironment("agent"); got != lr2 {
		t.Fatal("expected new runtime to remain registered")
	}
}

func TestFindReusableEnvironmentRequiresResolvedImageGeneration(t *testing.T) {
	lm := NewEnvironmentCache(&mockMounter{
		resolveFunc: func(cfg RootfsConfig) (RootfsConfig, error) {
			cfg.ImageCacheKey = "example.local/agent:dev@sha256:111"
			return cfg, nil
		},
	})
	fr := &api.ResolvedEnvironment{
		ID: "agent",
		Rootfs: &api.RootfsConfig{
			Type:   api.RootfsSrcType_IMAGE,
			Source: &api.RootfsConfig_ImageUrl{ImageUrl: "example.local/agent:dev"},
		},
		Argv: []string{"/bin/sh"},
	}
	environment, err := addTestEnvironmentCache(lm, fr)
	if err != nil {
		t.Fatalf("PrepareEnvironment() error = %v", err)
	}

	requested, err := RootfsConfigFromResolvedEnvironment(fr)
	if err != nil {
		t.Fatalf("RootfsConfigFromResolvedEnvironment() error = %v", err)
	}
	if got := lm.FindReusableEnvironment(fr, requested); got != nil {
		t.Fatal("unresolved mutable image ref must not reuse a mounted runtime")
	}
	requested, err = lm.ResolveRootfsConfig(requested)
	if err != nil {
		t.Fatalf("ResolveRootfsConfig() error = %v", err)
	}
	if got := lm.FindReusableEnvironment(fr, requested); got != environment {
		t.Fatal("resolved generation should reuse the mounted runtime")
	}

	drifted := proto.Clone(fr).(*api.ResolvedEnvironment)
	drifted.Argv = []string{"/bin/bash"}
	if got := lm.FindReusableEnvironment(drifted, requested); got != nil {
		t.Fatal("expected resolved specification drift to reject runtime reuse")
	}
	requested.ImageUrl = "example.local/agent:next"
	if got := lm.FindReusableEnvironment(fr, requested); got != nil {
		t.Fatal("expected image source drift to reject runtime reuse")
	}
}

func TestPrepareEnvironment_SharedRootfs(t *testing.T) {
	mock := &mockMounter{}
	lm := NewEnvironmentCache(mock)

	fr1 := newTestFR("rt-1", "/shared/path")
	fr2 := newTestFR("rt-2", "/shared/path")

	lr1, err := addTestEnvironmentCache(lm, fr1)
	if err != nil {
		t.Fatalf("PrepareEnvironment rt-1 failed: %v", err)
	}
	lr2, err := addTestEnvironmentCache(lm, fr2)
	if err != nil {
		t.Fatalf("PrepareEnvironment rt-2 failed: %v", err)
	}

	if lr1.RootFS != lr2.RootFS {
		t.Fatal("expected shared rootfs for same config")
	}
	if mock.MountCount() != 1 {
		t.Fatalf("expected 1 mount call, got %d", mock.MountCount())
	}
}

func TestList(t *testing.T) {
	lm := NewEnvironmentCache(&mockMounter{})

	addTestEnvironmentCache(lm, newTestFR("rt-1", "/p1"))
	addTestEnvironmentCache(lm, newTestFR("rt-2", "/p2"))

	list := lm.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 runtimes, got %d", len(list))
	}
}

func TestPreparedEnvironmentLoadOrPrepareBundleTemplateReusesPreparedTemplate(t *testing.T) {
	environment := &PreparedEnvironment{}
	loader, err := runtimeoci.NewBundleLoader("", t.TempDir())
	if err != nil {
		t.Fatalf("NewBundleLoader() error = %v", err)
	}

	prepareCalls := 0
	prepare := func() (*runtimeoci.BundleTemplate, error) {
		prepareCalls++
		return loader.PrepareBundleTemplate(runtimeoci.TemplateOptions{
			Request: &apipb.CreateContainerRequest{
				Command: []string{"/bin/sh"},
				Rootfs:  &apipb.Rootfs{RootDir: t.TempDir(), Readonly: true},
			},
		})
	}

	first, reused, err := environment.LoadOrPrepareBundleTemplate(prepare)
	if err != nil {
		t.Fatalf("LoadOrPrepareBundleTemplate(first) error = %v", err)
	}
	if reused {
		t.Fatal("expected first template load to be a miss")
	}
	second, reused, err := environment.LoadOrPrepareBundleTemplate(prepare)
	if err != nil {
		t.Fatalf("LoadOrPrepareBundleTemplate(second) error = %v", err)
	}
	if !reused {
		t.Fatal("expected second template load to be reused")
	}
	if first != second {
		t.Fatal("expected template pointer reuse")
	}
	if prepareCalls != 1 {
		t.Fatalf("prepare called %d times, want 1", prepareCalls)
	}
}

func TestPreparedEnvironmentResolvedEnvironmentRoundTripIncludesCwdAndMounts(t *testing.T) {
	lm := NewEnvironmentCache(&mockMounter{})

	fr := newTestFR("rt-roundtrip", "/roundtrip")
	fr.Cwd = "/workspace"
	fr.Env = map[string]string{"FOO": "bar"}
	fr.Mounts = []*api.Mount{
		{Type: "bind", Source: "/host/data", Target: "/data", Options: []string{"ro"}},
	}

	environment, err := addTestEnvironmentCache(lm, fr)
	if err != nil {
		t.Fatalf("PrepareEnvironment() error = %v", err)
	}

	got := environment.ResolvedEnvironment()
	if !proto.Equal(got, fr) {
		t.Fatalf("ResolvedEnvironment() = %v, want %v", got, fr)
	}
	if !environment.MatchesResolvedEnvironment(fr) {
		t.Fatal("expected PreparedEnvironment to match original ResolvedEnvironment")
	}

	drifted := proto.Clone(fr).(*api.ResolvedEnvironment)
	drifted.Cwd = "/workspace-2"
	if environment.MatchesResolvedEnvironment(drifted) {
		t.Fatal("expected drifted ResolvedEnvironment to mismatch")
	}
}

func TestMountError(t *testing.T) {
	mock := &mockMounter{mountErr: fmt.Errorf("mount failed")}
	lm := NewEnvironmentCache(mock)

	_, err := addTestEnvironmentCache(lm, newTestFR("rt-1", "/fail"))
	if err == nil {
		t.Fatal("expected error from PrepareEnvironment with failing mounter")
	}

	if lm.GetPreparedEnvironment("rt-1") != nil {
		t.Fatal("failed runtime should not be in map")
	}
}
