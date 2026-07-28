package client

import (
	"testing"
	"time"
)

func TestDeleteRPCTimeoutLeavesFastForceDeleteUnchanged(t *testing.T) {
	client := &Client{timeout: 10 * time.Second}

	got := client.deleteRPCTimeout(0)
	if got != 10*time.Second {
		t.Fatalf("deleteRPCTimeout(0) = %v, want %v", got, 10*time.Second)
	}
}

func TestDeleteRPCTimeoutExtendsPastGracePeriod(t *testing.T) {
	client := &Client{timeout: 10 * time.Second}

	got := client.deleteRPCTimeout(10)
	want := 20 * time.Second
	if got != want {
		t.Fatalf("deleteRPCTimeout(10) = %v, want %v", got, want)
	}
}

func TestDeleteRPCTimeoutKeepsLargerUserTimeout(t *testing.T) {
	client := &Client{timeout: 45 * time.Second}

	got := client.deleteRPCTimeout(10)
	want := 45 * time.Second
	if got != want {
		t.Fatalf("deleteRPCTimeout(10) = %v, want %v", got, want)
	}
}
