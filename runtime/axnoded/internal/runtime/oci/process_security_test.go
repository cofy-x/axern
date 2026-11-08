package oci

import (
	apipb "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	spec "github.com/opencontainers/runtime-spec/specs-go"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestProcessSecurityCannotBeDisabledByBaseOrCachedTemplate(t *testing.T) {
	require.True(t, defaultBundleSpec().Process.NoNewPrivileges)
	for _, uid := range []uint32{0, 1000} {
		for _, cached := range []bool{false, true} {
			loader, err := NewBundleLoader(t.TempDir())
			require.NoError(t, err)
			loader.baseSpec.Process.NoNewPrivileges = false
			loader.baseSpec.Process.User = spec.User{UID: uid, GID: 0, AdditionalGids: []uint32{44}}
			loader.baseSpec.Process.Capabilities = &spec.LinuxCapabilities{
				Bounding: []string{"CAP_SYS_ADMIN"}, Effective: []string{"CAP_SYS_ADMIN"}, Permitted: []string{"CAP_SYS_ADMIN"},
				Inheritable: []string{"CAP_SYS_ADMIN"}, Ambient: []string{"CAP_SYS_ADMIN"},
			}
			request := &apipb.CreateContainerRequest{Command: []string{"/bin/true"}}
			var generated *spec.Spec
			if cached {
				template, err := loader.PrepareBundleTemplate(TemplateOptions{Request: request})
				require.NoError(t, err)
				template.spec.Process.NoNewPrivileges = false
				template.spec.Process.Capabilities.Ambient = []string{"CAP_SYS_ADMIN"}
				_, generated, err = loader.MaterializeBundle(template, LoadOptions{ContainerID: "cached", Request: request})
				require.NoError(t, err)
			} else {
				_, generated, err = loader.Generate(LoadOptions{ContainerID: "direct", Request: request})
				require.NoError(t, err)
			}
			require.True(t, generated.Process.NoNewPrivileges)
			require.NoError(t, ValidateProcessSecurity(generated.Process))
			if uid == 0 {
				require.Equal(t, defaultLinuxCapabilities, generated.Process.Capabilities.Effective)
				require.NotContains(t, generated.Process.Capabilities.Bounding, "CAP_SYS_ADMIN")
			} else {
				require.EqualValues(t, 0, generated.Process.User.GID, "explicit primary group is retained")
				require.Empty(t, generated.Process.User.AdditionalGids)
				require.Empty(t, generated.Process.Capabilities.Bounding)
				require.Empty(t, generated.Process.Capabilities.Effective)
				require.Empty(t, generated.Process.Capabilities.Permitted)
				require.Empty(t, generated.Process.Capabilities.Inheritable)
				require.Empty(t, generated.Process.Capabilities.Ambient)
			}
		}
	}
}

func TestPersistedWeakProcessPolicyIsRejectedWithoutRepair(t *testing.T) {
	for _, mutate := range []func(*spec.Process){
		func(p *spec.Process) { p.NoNewPrivileges = false },
		func(p *spec.Process) { p.User.UID = 1000; p.Capabilities.Bounding = []string{"CAP_CHOWN"} },
		func(p *spec.Process) { p.User.UID = 1000; p.User.AdditionalGids = []uint32{0} },
		func(p *spec.Process) { p.Capabilities.Ambient = []string{"CAP_CHOWN"} },
	} {
		process := &spec.Process{NoNewPrivileges: true, Capabilities: &spec.LinuxCapabilities{}}
		mutate(process)
		require.Error(t, ValidateProcessSecurity(process))
	}
}
