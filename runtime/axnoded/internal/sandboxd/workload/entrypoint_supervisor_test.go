package workload

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

func TestSupervisorRetainsResultAfterDelivery(t *testing.T) {
	w := proc.NewWaiter(context.Background())
	defer w.Stop()
	s := NewSupervisor(Entrypoint{Args: []string{"/bin/sh", "-c", "exit 23"}}, time.Second, NewState(""), w, os.Stdout, os.Stderr)
	if result := <-s.Start(); result.ExitCode != 23 {
		t.Fatalf("start result: %+v", result)
	}
	results := make(chan ProcessResult, 2)
	for i := 0; i < 2; i++ {
		go func() { results <- s.Shutdown(os.Kill) }()
	}
	for i := 0; i < 2; i++ {
		if result := <-results; result.ExitCode != 23 || result.Err != nil {
			t.Fatalf("shutdown result: %+v", result)
		}
	}
	if s.Start() != nil {
		t.Fatal("restarted closed supervisor")
	}
}
