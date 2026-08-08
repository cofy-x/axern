package runtime

import "testing"

func TestRunscSandboxdArgsEnableHostUDS(t *testing.T) {
	base := []string{"--overlay2=root:dir=/filestore/runsc,size=1048576"}
	got := runscSandboxdArgs(base)
	if len(got) != 2 || got[0] != "--host-uds=create" || got[1] != base[0] {
		t.Fatalf("args = %#v", got)
	}
}

func TestRunscLifecycleArgsEnableConfiguredRuntimeFlags(t *testing.T) {
	handler := &RunscServiceHandler{ignoreCgroups: true, allowSUID: true}
	got := handler.lifecycleArgs("run", "cid")
	want := []string{"--ignore-cgroups", "--allow-suid", "run", "cid"}
	if len(got) != len(want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %#v, want %#v", got, want)
		}
	}
}

func TestRunscLifecycleArgsAllowSUIDCanBeDisabled(t *testing.T) {
	handler := &RunscServiceHandler{ignoreCgroups: true}
	got := handler.lifecycleArgs("run", "cid")
	want := []string{"--ignore-cgroups", "run", "cid"}
	if len(got) != len(want) {
		t.Fatalf("args = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %#v, want %#v", got, want)
		}
	}
}
