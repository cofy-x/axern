package service

import (
	"reflect"
	"strings"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
	"github.com/cofy-x/axern/runtime/axnoded/internal/container"
	"github.com/cofy-x/axern/runtime/axnoded/internal/resources"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/handlerregistry"
)

func TestNodeResourceProviderRejectsUnknownSource(t *testing.T) {
	_, err := nodeResourceProvider(config.PluginConfig{
		ControlPlaneNodeResourceSource: "kubernets",
	})
	if err == nil {
		t.Fatal("expected invalid node resource source to fail")
	}
	if !strings.Contains(err.Error(), "control_plane_node_resource_source") {
		t.Fatalf("error = %q, want control_plane_node_resource_source context", err.Error())
	}
}

func TestValidateRuntimeResourceConfigurationRejectsRequiredDisabledPool(t *testing.T) {
	registry := handlerregistry.New(config.Config{})
	registry.Set("runsc", &runtimeSpyHandler{
		name: "runsc",
		requirements: contract.RuntimeRequirements{
			Resources: []resources.ResourceName{resources.InterfaceResourceName},
		},
	})

	err := validateRuntimeResourceConfiguration(registry, config.ResourceConfig{
		MaxInstanceNum:     8,
		CgroupCacheSize:    8,
		InterfaceCacheSize: 0,
	})
	if err == nil || !strings.Contains(err.Error(), `runtime "runsc" requires disabled resource pool "interface"`) {
		t.Fatalf("validateRuntimeResourceConfiguration() error = %v", err)
	}
}

func TestValidateRuntimeResourceConfigurationAllowsUnusedDisabledPool(t *testing.T) {
	registry := handlerregistry.New(config.Config{})
	registry.Set("runsc", &runtimeSpyHandler{
		name: "runsc",
		requirements: contract.RuntimeRequirements{
			Resources: []resources.ResourceName{resources.InterfaceResourceName},
		},
	})

	if err := validateRuntimeResourceConfiguration(registry, config.ResourceConfig{
		MaxInstanceNum:     8,
		CgroupCacheSize:    0,
		InterfaceCacheSize: 8,
	}); err != nil {
		t.Fatalf("validateRuntimeResourceConfiguration() error = %v", err)
	}
}

func TestValidateRuntimeResourceConfigurationRejectsCapacityAboveContainerLimit(t *testing.T) {
	err := validateRuntimeResourceConfiguration(handlerregistry.New(config.Config{}), config.ResourceConfig{
		MaxInstanceNum: container.MaxContainerNum + 1,
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds container hard limit") {
		t.Fatalf("validateRuntimeResourceConfiguration() error = %v", err)
	}
}

func TestValidateRuntimeResourceConfigurationRejectsInvalidPoolSizes(t *testing.T) {
	tests := []struct {
		name string
		cfg  config.ResourceConfig
		want string
	}{
		{
			name: "non-positive instance capacity",
			cfg:  config.ResourceConfig{},
			want: "max_instance_num must be positive",
		},
		{
			name: "negative cache target",
			cfg:  config.ResourceConfig{MaxInstanceNum: 8, CgroupCacheSize: -1},
			want: "cgroup_cache_size must not be negative",
		},
		{
			name: "cache target above instance capacity",
			cfg:  config.ResourceConfig{MaxInstanceNum: 8, CgroupCacheSize: 9},
			want: "cgroup_cache_size 9 exceeds max_instance_num 8",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRuntimeResourceConfiguration(handlerregistry.New(config.Config{}), tt.cfg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validateRuntimeResourceConfiguration() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDisabledResourcePoolsFollowsConfiguredPoolSizes(t *testing.T) {
	got := disabledResourcePools(config.ResourceConfig{
		CgroupCacheSize:    0,
		InterfaceCacheSize: 8,
	})
	want := []resources.ResourceName{resources.CgroupResourceName}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("disabledResourcePools() = %v, want %v", got, want)
	}
}
