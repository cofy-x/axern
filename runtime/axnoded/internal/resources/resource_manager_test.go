//go:build linux

package resources

import (
	"testing"

	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/observability/metrics"
	"github.com/cofy-x/axern/runtime/axnoded/internal/storetest"
	"github.com/stretchr/testify/assert"
)

func TestNewManager(t *testing.T) {
	metrics.ResetForTest()

	manager, err := NewResourceManager(storetest.NewMockStore(), config.Config{
		PluginConfig: config.PluginConfig{
			ResourceConfig: config.ResourceConfig{
				MaxInstanceNum:  10,
				CgroupRootName:  "sandbox",
				CgroupCacheSize: 8,
				// currently, interface can not be created in test env
				InterfaceCacheSize: 0,
			},
		},
	})
	assert.Nil(t, err)
	assert.NotNil(t, manager)

	assert.Equal(t, float64(8), metrics.GaugeValueForTest(metrics.MetricSandboxResourceCurrent, map[string]string{
		sdkobs.AttrResource: "cgroup",
	}))
	// TODO: test interface
	// assert.Equal(t, float64(1), metrics.GaugeValueForTest(metrics.MetricSandboxResourceCurrent, map[string]string{sdkobs.AttrResource: "interface"}))
}
