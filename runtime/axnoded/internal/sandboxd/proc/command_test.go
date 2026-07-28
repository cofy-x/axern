package proc

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestRunShellOutputUsesSharedWaiter(t *testing.T) {
	waiter := NewWaiter(context.Background())
	defer waiter.Stop()

	output, err := RunShellOutput(context.Background(), waiter, "printf '%s' \"$AXERN_TEST_VALUE\"", []string{"AXERN_TEST_VALUE=ok"}, time.Second)
	if err != nil {
		t.Fatalf("RunShellOutput() error = %v", err)
	}
	if string(output) != "ok" {
		t.Fatalf("output = %q", output)
	}
}

func TestRunShellOutputHandlesShortLivedNoOutputCommand(t *testing.T) {
	waiter := NewWaiter(context.Background())
	defer waiter.Stop()

	for i := 0; i < 50; i++ {
		output, err := RunShellOutput(context.Background(), waiter, "true", nil, time.Second)
		if err != nil {
			t.Fatalf("RunShellOutput() iteration %d error = %v", i, err)
		}
		if len(output) != 0 {
			t.Fatalf("output iteration %d = %q", i, output)
		}
	}
}

func TestRunShellOutputTimeoutKillsProcess(t *testing.T) {
	waiter := NewWaiter(context.Background())
	defer waiter.Stop()

	_, err := RunShellOutput(context.Background(), waiter, "sleep 5", nil, 10*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "deadline exceeded") {
		t.Fatalf("RunShellOutput() error = %v, want deadline exceeded", err)
	}
}
