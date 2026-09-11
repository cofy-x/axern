//go:build linux

package resources

import (
	"errors"
	"testing"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	"github.com/stretchr/testify/require"
)

func TestNewResourceManagerComposesCgroupPoolWithoutHostCgroupAccess(t *testing.T) {
	metrics.ResetForTest()
	cgroup := &MockResourceManager{maxSize: 10, maxCacheSize: 8, name: string(CgroupResourceName)}

	managers, err := newResourceManager(storetest.NewMockStore(), config.Config{
		PluginConfig: config.PluginConfig{
			ResourceConfig: config.ResourceConfig{
				MaxInstanceNum:  10,
				CgroupRootName:  "sandbox",
				CgroupCacheSize: 8,
				// currently, interface can not be created in test env
				InterfaceCacheSize: 0,
			},
		},
	}, func(stateStore, int, int, config.NetworkConfig) (Manager, error) {
		return nil, errors.New("network factory should not be called")
	}, func(stateStore, config.ResourceConfig, bool) (resizable, error) {
		return cgroup, nil
	})
	require.NoError(t, err)
	require.Len(t, managers, 1)
	require.Same(t, cgroup, managers[0])
	t.Cleanup(func() { require.NoError(t, cgroup.ShutDown()) })

	require.Equal(t, float64(8), metrics.GaugeValueForTest(metrics.MetricSandboxResourceCurrent, map[string]string{
		sdkobs.AttrResource: "cgroup",
	}))
}
