package bpfnet

import (
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type fakeDataplane struct {
	ensureErr   error
	ipRange     string
	nativeCIDRs []string
	gcPolicy    SNATGCPolicy
	gcResult    SNATGCResult
	gcErr       error
}

func (f *fakeDataplane) EnsureAttached(_ []string, ipRange string, nativeCIDRs []string) (dataplaneAttachment, error) {
	f.ipRange = ipRange
	f.nativeCIDRs = append([]string(nil), nativeCIDRs...)
	return dataplaneAttachment{}, f.ensureErr
}

func (f *fakeDataplane) CleanupStaleSNATMappings(policy SNATGCPolicy) (SNATGCResult, error) {
	f.gcPolicy = policy
	return f.gcResult, f.gcErr
}

func TestConfigWithDefaults(t *testing.T) {
	cfg := (Config{}).WithDefaults()
	if cfg.PinPath != DefaultPinPath || cfg.StatePath != DefaultStatePath || cfg.SNATMapSize != DefaultSNATMapSize {
		t.Fatalf("unexpected defaults: %#v", cfg)
	}
}

func TestControllerPersistsEgressOnlyReadiness(t *testing.T) {
	root := t.TempDir()
	dp := &fakeDataplane{}
	ctrl := NewController(Config{
		PinPath:            filepath.Join(root, "pins"),
		StatePath:          filepath.Join(root, "state"),
		UplinkDevices:      []string{"eth0"},
		NativeRoutingCIDRs: []string{"10.0.0.0/8"},
	})
	ctrl.dp = dp

	if err := ctrl.EnsureAttached("172.31.0.1/16"); err != nil {
		t.Fatal(err)
	}
	status, err := ctrl.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.State.Mode != ModeEgressSNAT || !status.State.TCReady || !status.State.EgressSNAT {
		t.Fatalf("unexpected state: %#v", status.State)
	}
	if dp.ipRange != "172.31.0.1/16" || !reflect.DeepEqual(dp.nativeCIDRs, []string{"10.0.0.0/8"}) {
		t.Fatalf("unexpected dataplane input: ip=%q native=%v", dp.ipRange, dp.nativeCIDRs)
	}
}

func TestControllerPersistsAttachFailure(t *testing.T) {
	root := t.TempDir()
	ctrl := NewController(Config{PinPath: filepath.Join(root, "pins"), StatePath: filepath.Join(root, "state"), UplinkDevices: []string{"eth0"}})
	ctrl.dp = &fakeDataplane{ensureErr: errors.New("attach failed")}
	if err := ctrl.EnsureAttached("172.31.0.1/16"); err == nil {
		t.Fatal("expected attach failure")
	}
	status, err := ctrl.Status()
	if err != nil {
		t.Fatal(err)
	}
	if status.State.Mode != ModeAttachFailed || status.State.TCReady || status.State.LastAttachError == "" {
		t.Fatalf("unexpected failed state: %#v", status.State)
	}
}

func TestControllerDelegatesSNATGC(t *testing.T) {
	policy := SNATGCPolicy{TCPIdleTimeout: time.Minute}
	want := SNATGCResult{FwdScanned: 3, FwdDeleted: 1}
	dp := &fakeDataplane{gcResult: want}
	ctrl := NewController(Config{})
	ctrl.dp = dp
	got, err := ctrl.CleanupStaleSNATMappings(policy)
	if err != nil || got != want || dp.gcPolicy != policy {
		t.Fatalf("gc result=%#v policy=%#v err=%v", got, dp.gcPolicy, err)
	}
}
