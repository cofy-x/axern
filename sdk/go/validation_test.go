package axernsdk

import (
	"context"
	"testing"
	"time"
)

func TestSandboxOptionValidation(t *testing.T) {
	_, err := NewSandbox(SandboxOptions{TemplateID: "python311"})
	if !IsValidation(err) {
		t.Fatalf("NewSandbox without client error = %v, want validation", err)
	}
	_, err = NewSandbox(SandboxOptions{Client: &Client{}, TemplateID: "python311", ReadyTimeout: -time.Second})
	if !IsValidation(err) {
		t.Fatalf("NewSandbox negative ready timeout error = %v, want validation", err)
	}
	_, err = NewSandbox(SandboxOptions{Client: &Client{}, TemplateID: "python311", RequestCPU: "-1"})
	if !IsValidation(err) {
		t.Fatalf("NewSandbox negative cpu error = %v, want validation", err)
	}
	_, err = NewSandbox(SandboxOptions{Client: &Client{}, TemplateID: "python311", ImageMounts: []ImageMount{{Image: "tool", Target: "/usr"}}})
	if !IsValidation(err) {
		t.Fatalf("NewSandbox protected image mount target error = %v, want validation", err)
	}
}

func TestExecAndProcessOptionValidation(t *testing.T) {
	node := &AllocationClient{allocationID: "alloc-1"}
	if _, err := node.Exec(context.Background(), "", ExecOptions{}); !IsValidation(err) {
		t.Fatalf("Exec empty command error = %v, want validation", err)
	}
	if _, err := node.Exec(context.Background(), "true", ExecOptions{Timeout: -time.Second}); !IsValidation(err) {
		t.Fatalf("Exec negative timeout error = %v, want validation", err)
	}
	if _, err := node.Process(context.Background(), []string{}, ProcessOptions{}); !IsValidation(err) {
		t.Fatalf("Process empty argv error = %v, want validation", err)
	}
	if _, err := node.Process(context.Background(), "true", ProcessOptions{Timeout: -time.Second}); !IsValidation(err) {
		t.Fatalf("Process negative timeout error = %v, want validation", err)
	}
	if _, err := node.Process(context.Background(), "true", ProcessOptions{InitialCols: 80}); !IsValidation(err) {
		t.Fatalf("Process incomplete initial size error = %v, want validation", err)
	}
}

func TestAllocationClientValidation(t *testing.T) {
	if _, err := (*Client)(nil).Allocation("alloc-1"); !IsValidation(err) {
		t.Fatalf("nil client NodeSandbox error = %v, want validation", err)
	}
	if _, err := (&Client{}).Allocation(""); !IsValidation(err) {
		t.Fatalf("empty allocation NodeSandbox error = %v, want validation", err)
	}
	if _, err := (&AllocationClient{}).Exec(context.Background(), Args("true"), ExecOptions{}); !IsValidation(err) {
		t.Fatalf("zero node client Exec error = %v, want validation", err)
	}
}

func TestFileAndTunnelValidation(t *testing.T) {
	node := &AllocationClient{allocationID: "alloc-1"}
	if err := node.Chmod(context.Background(), "/tmp/x", 0o10000, ChmodOptions{}); !IsValidation(err) {
		t.Fatalf("Chmod invalid mode error = %v, want validation", err)
	}
	if err := validateTunnelOptions(TunnelOptions{Upstream: "127.0.0.1:8080", ProxyPort: -1}); !IsValidation(err) {
		t.Fatalf("negative proxy port error = %v, want validation", err)
	}
	if err := validateTunnelOptions(TunnelOptions{Upstream: " ", ReadyTimeout: time.Second}); !IsValidation(err) {
		t.Fatalf("blank upstream error = %v, want validation", err)
	}
}
