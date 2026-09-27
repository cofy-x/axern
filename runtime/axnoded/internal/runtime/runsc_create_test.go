package runtime

import (
	"reflect"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
)

func TestRunscCreateUsesLoopbackOnlyForIsolatedAllocation(t *testing.T) {
	handler := &RunscServiceHandler{ignoreCgroups: true}
	for _, test := range []struct {
		name    string
		options contract.HandlerOptions
		want    []string
	}{
		{
			name:    "isolated",
			options: contract.HandlerOptions{NetworkMode: commonv1.NetworkMode_NETWORK_MODE_ISOLATED},
			want:    []string{"--ignore-cgroups", "--network=none", "--overlay2=root:dir=/filestore/runsc,size=1024", "create", "--pid-file", "/pid", "--bundle", "/bundle", "alloc-test"},
		},
		{
			name:    "connected",
			options: contract.HandlerOptions{NetworkMode: commonv1.NetworkMode_NETWORK_MODE_DEFAULT, NetworkNamespacePath: "/run/netns/alloc-test"},
			want:    []string{"--ignore-cgroups", "--overlay2=root:dir=/filestore/runsc,size=1024", "create", "--pid-file", "/pid", "--bundle", "/bundle", "alloc-test"},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := handler.preparedContainerCreateArgs(test.options, []string{"--overlay2=root:dir=/filestore/runsc,size=1024"}, "/pid", "/bundle", "alloc-test")
			if err != nil {
				t.Fatalf("preparedContainerCreateArgs() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("args = %#v, want %#v", got, test.want)
			}
		})
	}
	if _, err := handler.preparedContainerCreateArgs(contract.HandlerOptions{
		NetworkMode:          commonv1.NetworkMode_NETWORK_MODE_ISOLATED,
		NetworkNamespacePath: "/run/netns/alloc-test",
	}, nil, "/pid", "/bundle", "alloc-test"); err == nil {
		t.Fatal("isolated allocation with connected namespace was accepted")
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
