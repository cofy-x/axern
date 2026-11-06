package config

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNativeRecoveryRequiresExplicitOptInAndRequiredCgroups(t *testing.T) {
	require.False(t, DefaultConfig().RuntimeConfig.NativeRecovery)
	decoded, err := Decode([]byte("[plugin.runtime]\nnative_recovery = true\ncgroup_enforcement = 'required'\n"))
	require.NoError(t, err)
	require.True(t, decoded.RuntimeConfig.NativeRecovery)
	_, err = Decode([]byte("[plugin.runtime]\nnative_recovery = true\ncgroup_enforcement = 'disabled_dev'\n"))
	require.ErrorContains(t, err, "native_recovery requires")
	require.NoError(t, (RuntimeConfig{CgroupEnforcement: CgroupEnforcementDisabledDev}).ValidateNativeRecovery())
}
