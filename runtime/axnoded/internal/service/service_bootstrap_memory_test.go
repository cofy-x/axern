package service

import (
	"strings"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/config"
)

func TestValidateMemoryBoundaryConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		mode     string
		reserve  int64
		rootName string
		wantErr  string
	}{
		{name: "required qualified", mode: config.CgroupEnforcementRequired, reserve: 512 << 20},
		{name: "required missing", mode: config.CgroupEnforcementRequired, wantErr: "at least"},
		{name: "required below certification reserve", mode: config.CgroupEnforcementRequired, reserve: config.RuntimeConformanceMemoryMaxBytes - 1, wantErr: "at least"},
		{name: "development zero", mode: config.CgroupEnforcementDisabledDev},
		{name: "development positive", mode: config.CgroupEnforcementDisabledDev, reserve: 1, wantErr: "must be zero"},
		{name: "development negative", mode: config.CgroupEnforcementDisabledDev, reserve: -1, wantErr: "must be zero"},
		{name: "unknown mode", mode: "best_effort", reserve: 512 << 20, wantErr: "cgroup_enforcement"},
		{name: "absolute root rejected", mode: config.CgroupEnforcementRequired, reserve: 512 << 20, rootName: "/sandbox", wantErr: "single child"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.Config{PluginConfig: config.PluginConfig{
				RuntimeConfig:  config.RuntimeConfig{CgroupEnforcement: tt.mode},
				ResourceConfig: config.ResourceConfig{MemorySystemReserveBytes: tt.reserve, CgroupRootName: tt.rootName},
			}}
			err := validateMemoryBoundaryConfiguration(cfg)
			if tt.wantErr == "" && err != nil {
				t.Fatalf("validateMemoryBoundaryConfiguration() error = %v", err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("validateMemoryBoundaryConfiguration() error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
