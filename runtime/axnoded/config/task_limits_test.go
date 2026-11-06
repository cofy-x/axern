package config

import "testing"

func TestRuntimeTaskLimitsRequirePairedBoundedRequiredPolicy(t *testing.T) {
	for _, test := range []struct {
		name        string
		guest, host int64
		mode        string
		wantErr     bool
	}{
		{name: "default disabled"},
		{name: "development disabled", mode: CgroupEnforcementDisabledDev},
		{name: "native profile", guest: 256, host: 1024},
		{name: "minimum", guest: 64, host: 64},
		{name: "maximum", guest: 65536, host: 65536},
		{name: "guest only", guest: 256, wantErr: true},
		{name: "host only", host: 1024, wantErr: true},
		{name: "negative", guest: -1, host: 1024, wantErr: true},
		{name: "guest too small", guest: 63, host: 1024, wantErr: true},
		{name: "host too small", guest: 256, host: 63, wantErr: true},
		{name: "guest too large", guest: 65537, host: 1024, wantErr: true},
		{name: "host too large", guest: 256, host: 65537, wantErr: true},
		{name: "development cannot enforce", guest: 256, host: 1024, mode: CgroupEnforcementDisabledDev, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := RuntimeConfig{GuestTaskLimit: test.guest, HostTaskLimit: test.host, CgroupEnforcement: test.mode}
			if err := cfg.ValidateTaskLimits(); (err != nil) != test.wantErr {
				t.Fatalf("ValidateTaskLimits() error = %v, wantErr %t", err, test.wantErr)
			}
		})
	}
	cfg := DefaultConfig()
	if cfg.RuntimeConfig.GuestTaskLimit != 0 || cfg.RuntimeConfig.HostTaskLimit != 0 {
		t.Fatal("default profiles must explicitly opt in to task enforcement")
	}
}
