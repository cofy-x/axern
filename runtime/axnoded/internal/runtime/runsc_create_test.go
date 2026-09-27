package runtime

import (
	"reflect"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestRunscCreateAndStartUseSameNetworkMode(t *testing.T) {
	handler := &RunscServiceHandler{ignoreCgroups: true}
	for _, test := range []struct {
		name       string
		options    contract.HandlerOptions
		wantCreate []string
		wantStart  []string
	}{
		{
			name:       "isolated",
			options:    contract.HandlerOptions{NetworkMode: commonv1.NetworkMode_NETWORK_MODE_ISOLATED},
			wantCreate: []string{"--ignore-cgroups", "--network=none", "--overlay2=root:dir=/filestore/runsc,size=1024", "create", "--pid-file", "/pid", "--bundle", "/bundle", "alloc-test"},
			wantStart:  []string{"--ignore-cgroups", "--network=none", "start", "alloc-test"},
		},
		{
			name:       "connected",
			options:    contract.HandlerOptions{NetworkMode: commonv1.NetworkMode_NETWORK_MODE_DEFAULT, NetworkNamespacePath: "/run/netns/alloc-test"},
			wantCreate: []string{"--ignore-cgroups", "--overlay2=root:dir=/filestore/runsc,size=1024", "create", "--pid-file", "/pid", "--bundle", "/bundle", "alloc-test"},
			wantStart:  []string{"--ignore-cgroups", "start", "alloc-test"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := handler.preparedContainerCreateArgs(test.options, []string{"--overlay2=root:dir=/filestore/runsc,size=1024"}, "/pid", "/bundle", "alloc-test")
			if err != nil {
				t.Fatalf("preparedContainerCreateArgs() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.wantCreate) {
				t.Fatalf("create args = %#v, want %#v", got, test.wantCreate)
			}
			got, err = handler.preparedContainerLifecycleArgs(test.options, "alloc-test", "start", "alloc-test")
			if err != nil {
				t.Fatalf("preparedContainerLifecycleArgs(start) error = %v", err)
			}
			if !reflect.DeepEqual(got, test.wantStart) {
				t.Fatalf("start args = %#v, want %#v", got, test.wantStart)
			}
		})
	}
	conflicting := contract.HandlerOptions{
		NetworkMode:          commonv1.NetworkMode_NETWORK_MODE_ISOLATED,
		NetworkNamespacePath: "/run/netns/alloc-test",
	}
	if _, err := handler.preparedContainerCreateArgs(conflicting, nil, "/pid", "/bundle", "alloc-test"); err == nil {
		t.Fatal("create accepted isolated allocation with connected namespace")
	}
	if _, err := handler.preparedContainerLifecycleArgs(conflicting, "alloc-test", "start", "alloc-test"); err == nil {
		t.Fatal("start accepted isolated allocation with connected namespace")
	}
}

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
