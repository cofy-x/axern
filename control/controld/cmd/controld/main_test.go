package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseFlagsResourceCPUOvercommitRatio(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{
		"controld",
		"-postgres-dsn=postgres://test",
		"-secrets-master-key=test-only-master-key-32-bytes!!!",
		"-tls-ca-cert=ca.crt",
		"-tls-cert=server.crt",
		"-tls-key=server.key",
		"-resource-cpu-overcommit-ratio=2.5",
	}

	opts, err := parseFlags()
	if err != nil {
		t.Fatalf("parseFlags() error = %v", err)
	}
	if opts.resourceCPUOvercommitRatio != 2.5 {
		t.Fatalf("resourceCPUOvercommitRatio = %v, want 2.5", opts.resourceCPUOvercommitRatio)
	}
}

func TestParseFlagsRejectsInvalidResourceCPUOvercommitRatio(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{
		"controld",
		"-postgres-dsn=postgres://test",
		"-secrets-master-key=test-only-master-key-32-bytes!!!",
		"-tls-ca-cert=ca.crt",
		"-tls-cert=server.crt",
		"-tls-key=server.key",
		"-resource-cpu-overcommit-ratio=0",
	}

	_, err := parseFlags()
	if err == nil {
		t.Fatal("parseFlags() returned nil error")
	}
	if !strings.Contains(err.Error(), "resource-cpu-overcommit-ratio must be > 0") {
		t.Fatalf("parseFlags() error = %q", err)
	}
}

func TestParseFlagsRejectsNegativeReconcileTimeout(t *testing.T) {
	oldArgs := os.Args
	t.Cleanup(func() { os.Args = oldArgs })
	os.Args = []string{
		"controld",
		"-postgres-dsn=postgres://test",
		"-secrets-master-key=test-only-master-key-32-bytes!!!",
		"-tls-ca-cert=ca.crt",
		"-tls-cert=server.crt",
		"-tls-key=server.key",
		"-reconcile-timeout=" + (-time.Second).String(),
	}

	_, err := parseFlags()
	if err == nil {
		t.Fatal("parseFlags() returned nil error")
	}
	if !strings.Contains(err.Error(), "reconcile-timeout must be >= 0") {
		t.Fatalf("parseFlags() error = %q", err)
	}
}
