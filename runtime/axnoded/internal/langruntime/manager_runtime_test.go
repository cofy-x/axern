package langruntime

import (
	"context"
	"fmt"
	"testing"
	"time"

	api "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	runtimeoci "github.com/cofy-x/axern/runtime/axnoded/internal/runtime/oci"
	"google.golang.org/protobuf/proto"
)

func TestGetLangRuntime_NotFound(t *testing.T) {
	lm := NewLanguageRuntimeManager(&mockMounter{})
	lr := lm.GetLangRuntime("nonexistent")
	if lr != nil {
		t.Fatal("expected nil for nonexistent runtime")
	}
}

func TestAddAndGetLangRuntime(t *testing.T) {
	lm := NewLanguageRuntimeManager(&mockMounter{})

	fr := newTestFR("rt-1", "/some/path")
	lr, err := addTestLangRuntime(lm, fr, false)
	if err != nil {
		t.Fatalf("AddLangRuntime failed: %v", err)
	}
	if lr.ID != "rt-1" {
		t.Fatalf("expected ID rt-1, got %s", lr.ID)
	}

	got := lm.GetLangRuntime("rt-1")
	if got == nil {
		t.Fatal("expected to find runtime rt-1")
	}
	if got != lr {
		t.Fatal("expected same pointer")
	}
}

func TestAddLangRuntime_Duplicate(t *testing.T) {
	lm := NewLanguageRuntimeManager(&mockMounter{})

	fr := newTestFR("rt-1", "/some/path")
	lr1, err := addTestLangRuntime(lm, fr, false)
	if err != nil {
		t.Fatalf("first AddLangRuntime failed: %v", err)
	}

	lr2, err := addTestLangRuntime(lm, fr, false)
	if err != nil {
		t.Fatalf("second AddLangRuntime failed: %v", err)
	}
	if lr1 != lr2 {
		t.Fatal("expected same runtime for duplicate add")
	}
}

func TestAddLangRuntime_DriftedIdleRuntimeReplaced(t *testing.T) {
	mock := &mockMounter{}
	lm := NewLanguageRuntimeManager(mock)
	lm.ConfigureRetention(time.Minute, 8)

	fr := newTestFR("rt-drift", "/some/path")
	fr.Cwd = "/workspace-a"
	fr.Mounts = []*api.Mount{{Type: "bind", Source: "/host/a", Target: "/data", Options: []string{"ro"}}}
	lr1, err := addTestLangRuntime(lm, fr, true)
	if err != nil {
		t.Fatalf("AddLangRuntime(first) failed: %v", err)
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
	destroyCalls := 0
	if !lr1.BeginExecutionEnvelopePrepare() {
		t.Fatal("expected execution envelope prepare slot")
	}
	if !lr1.FinishExecutionEnvelopePrepare(&ExecutionEnvelope{
		Destroy: func(context.Context) error {
			destroyCalls++
			return nil
		},
	}) {
		t.Fatal("expected execution envelope to become ready")
	}

	drifted := newTestFR("rt-drift", "/other/path")
	drifted.Command = []string{"/bin/bash"}
	drifted.Cwd = "/workspace-b"
	drifted.RuntimeEnvs = map[string]string{"FOO": "bar"}
	drifted.Mounts = []*api.Mount{{Type: "bind", Source: "/host/b", Target: "/data", Options: []string{"rw"}}}
	lr2, created, err := addTestLangRuntimeWithState(lm, drifted, true)
	if err != nil {
		t.Fatalf("AddLangRuntime(drifted) failed: %v", err)
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
	if destroyCalls != 1 {
		t.Fatalf("execution envelope destroy calls = %d, want 1", destroyCalls)
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

func TestAddLangRuntime_DriftedActiveRuntimeSuperseded(t *testing.T) {
	lm := NewLanguageRuntimeManager(&mockMounter{})

	lr, err := addTestLangRuntime(lm, newTestFR("rt-active-drift", "/some/path"), true)
	if err != nil {
		t.Fatalf("AddLangRuntime() failed: %v", err)
	}
	lr.IncRef()

	drifted := newTestFR("rt-active-drift", "/other/path")
	drifted.Command = []string{"/bin/bash"}
	replacement, created, err := addTestLangRuntimeWithState(lm, drifted, true)
	if err != nil {
		t.Fatalf("AddLangRuntime(drifted) failed: %v", err)
	}
	if !created {
		t.Fatal("expected drifted active runtime to create replacement")
	}
	if replacement == lr {
		t.Fatal("expected replacement runtime pointer")
	}
	if current := lm.GetLangRuntime("rt-active-drift"); current != replacement {
		t.Fatal("expected replacement runtime to be registered")
	}
	if lr.Released() {
		t.Fatal("expected active superseded runtime to stay alive until its last ref is released")
	}
	lr.DecRef()
	if !lr.Released() {
		t.Fatal("expected superseded runtime to be released after its last ref")
	}
	if current := lm.GetLangRuntime("rt-active-drift"); current != replacement {
		t.Fatal("expected replacement runtime to remain registered after superseded runtime release")
	}
}

func TestAddLangRuntime_ImageCacheKeyDriftSupersedesActiveRuntime(t *testing.T) {
	currentCacheKey := "example.local/agent:dev@sha256:111"
	mock := &mockMounter{
		resolveFunc: func(cfg RootfsConfig) (RootfsConfig, error) {
			cfg.ImageCacheKey = currentCacheKey
			return cfg, nil
		},
	}
	lm := NewLanguageRuntimeManager(mock)
	fr := &api.RuntimeTemplate{
		ID:      "agent",
		Sandbox: "runsc",
		Rootfs: &api.RootfsConfig{
			Type:   api.RootfsSrcType_IMAGE,
			Source: &api.RootfsConfig_ImageUrl{ImageUrl: "example.local/agent:dev"},
		},
		Command: []string{"/bin/sh"},
	}

	lr1, err := addTestLangRuntime(lm, fr, true)
	if err != nil {
		t.Fatalf("AddLangRuntime(first) failed: %v", err)
	}
	lr1.IncRef()

	currentCacheKey = "example.local/agent:dev@sha256:222"
	lr2, created, err := addTestLangRuntimeWithState(lm, fr, true)
	if err != nil {
		t.Fatalf("AddLangRuntime(second) failed: %v", err)
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
	if got := lm.GetLangRuntime("agent"); got != lr2 {
		t.Fatal("expected new runtime to remain registered")
	}
}

func TestFindReusableLangRuntimeMatchesUnresolvedImageConfig(t *testing.T) {
	lm := NewLanguageRuntimeManager(&mockMounter{
		resolveFunc: func(cfg RootfsConfig) (RootfsConfig, error) {
			cfg.ImageCacheKey = "example.local/agent:dev@sha256:111"
			return cfg, nil
		},
	})
	fr := &api.RuntimeTemplate{
		ID:      "agent",
		Sandbox: "runsc",
		Rootfs: &api.RootfsConfig{
			Type:   api.RootfsSrcType_IMAGE,
			Source: &api.RootfsConfig_ImageUrl{ImageUrl: "example.local/agent:dev"},
		},
		Command: []string{"/bin/sh"},
	}
	lr, err := addTestLangRuntime(lm, fr, true)
	if err != nil {
		t.Fatalf("AddLangRuntime() error = %v", err)
	}

	requested, err := RootfsConfigFromRuntimeTemplate(fr)
	if err != nil {
		t.Fatalf("RootfsConfigFromRuntimeTemplate() error = %v", err)
	}
	if got := lm.FindReusableLangRuntime(fr, requested); got != lr {
		t.Fatal("expected unresolved image config to reuse the mounted runtime")
	}

	drifted := proto.Clone(fr).(*api.RuntimeTemplate)
	drifted.Command = []string{"/bin/bash"}
	if got := lm.FindReusableLangRuntime(drifted, requested); got != nil {
		t.Fatal("expected static template drift to reject runtime reuse")
	}
	requested.ImageUrl = "example.local/agent:next"
	if got := lm.FindReusableLangRuntime(fr, requested); got != nil {
		t.Fatal("expected image source drift to reject runtime reuse")
	}
}

func TestAddLangRuntime_SharedRootfs(t *testing.T) {
	mock := &mockMounter{}
	lm := NewLanguageRuntimeManager(mock)

	fr1 := newTestFR("rt-1", "/shared/path")
	fr2 := newTestFR("rt-2", "/shared/path")

	lr1, err := addTestLangRuntime(lm, fr1, false)
	if err != nil {
		t.Fatalf("AddLangRuntime rt-1 failed: %v", err)
	}
	lr2, err := addTestLangRuntime(lm, fr2, false)
	if err != nil {
		t.Fatalf("AddLangRuntime rt-2 failed: %v", err)
	}

	if lr1.RootFS != lr2.RootFS {
		t.Fatal("expected shared rootfs for same config")
	}
	if mock.MountCount() != 1 {
		t.Fatalf("expected 1 mount call, got %d", mock.MountCount())
	}
}

func TestList(t *testing.T) {
	lm := NewLanguageRuntimeManager(&mockMounter{})

	addTestLangRuntime(lm, newTestFR("rt-1", "/p1"), false)
	addTestLangRuntime(lm, newTestFR("rt-2", "/p2"), false)

	list := lm.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 runtimes, got %d", len(list))
	}
}

func TestLanguageRuntimeLoadOrPrepareBundleTemplateReusesPreparedTemplate(t *testing.T) {
	lr := &LanguageRuntime{}
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

	first, reused, err := lr.LoadOrPrepareBundleTemplate(prepare)
	if err != nil {
		t.Fatalf("LoadOrPrepareBundleTemplate(first) error = %v", err)
	}
	if reused {
		t.Fatal("expected first template load to be a miss")
	}
	second, reused, err := lr.LoadOrPrepareBundleTemplate(prepare)
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

func TestLanguageRuntimeRuntimeTemplateRoundTripIncludesCwdAndMounts(t *testing.T) {
	lm := NewLanguageRuntimeManager(&mockMounter{})

	fr := newTestFR("rt-roundtrip", "/roundtrip")
	fr.Cwd = "/workspace"
	fr.RuntimeEnvs = map[string]string{"FOO": "bar"}
	fr.Mounts = []*api.Mount{
		{Type: "bind", Source: "/host/data", Target: "/data", Options: []string{"ro"}},
	}

	lr, err := addTestLangRuntime(lm, fr, false)
	if err != nil {
		t.Fatalf("AddLangRuntime() error = %v", err)
	}

	got := lr.RuntimeTemplate()
	if !proto.Equal(got, fr) {
		t.Fatalf("RuntimeTemplate() = %v, want %v", got, fr)
	}
	if !lr.MatchesRuntimeTemplate(fr) {
		t.Fatal("expected LanguageRuntime to match original RuntimeTemplate")
	}

	drifted := proto.Clone(fr).(*api.RuntimeTemplate)
	drifted.Cwd = "/workspace-2"
	if lr.MatchesRuntimeTemplate(drifted) {
		t.Fatal("expected drifted RuntimeTemplate to mismatch")
	}
}

func TestMountError(t *testing.T) {
	mock := &mockMounter{mountErr: fmt.Errorf("mount failed")}
	lm := NewLanguageRuntimeManager(mock)

	_, err := addTestLangRuntime(lm, newTestFR("rt-1", "/fail"), false)
	if err == nil {
		t.Fatal("expected error from AddLangRuntime with failing mounter")
	}

	if lm.GetLangRuntime("rt-1") != nil {
		t.Fatal("failed runtime should not be in map")
	}
}
