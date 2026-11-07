package process

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

func TestWaitManagedProcessesCancellationDoesNotCompleteProcess(t *testing.T) {
	for _, cancelBeforeWait := range []bool{true, false} {
		name := "during wait"
		if cancelBeforeWait {
			name = "before wait"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				completed := &managedProcess{done: make(chan struct{})}
				pending := &managedProcess{done: make(chan struct{})}
				close(completed.done)
				if cancelBeforeWait {
					cancel()
				}
				result := make(chan bool, 1)
				go func() {
					result <- waitManagedProcesses(ctx, []*managedProcess{completed, pending}, time.Hour)
				}()
				synctest.Wait()
				if !cancelBeforeWait {
					select {
					case got := <-result:
						t.Fatalf("wait returned %v while a process is pending", got)
					default:
					}
					cancel()
					synctest.Wait()
				}
				if <-result {
					t.Fatal("cancellation reported successful process completion")
				}
				select {
				case <-pending.done:
					t.Fatal("wait changed process completion")
				default:
				}
			})
		})
	}
}

func TestWaitManagedProcessesWaitsForEveryProcess(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		first := &managedProcess{done: make(chan struct{})}
		second := &managedProcess{done: make(chan struct{})}
		result := make(chan bool, 1)
		go func() {
			result <- waitManagedProcesses(context.Background(), []*managedProcess{first, second}, time.Hour)
		}()
		close(second.done)
		synctest.Wait()
		select {
		case got := <-result:
			t.Fatalf("wait returned %v before every process finished", got)
		default:
		}
		close(first.done)
		if !<-result {
			t.Fatal("all processes completed but wait failed")
		}
	})
}

func TestWaitManagedProcessesSharesOneTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		first := &managedProcess{done: make(chan struct{})}
		second := &managedProcess{done: make(chan struct{})}
		go func() {
			time.Sleep(3 * time.Second)
			close(first.done)
		}()
		started := time.Now()
		if waitManagedProcesses(context.Background(), []*managedProcess{first, second}, 5*time.Second) {
			t.Fatal("timeout reported successful process completion")
		}
		if elapsed := time.Since(started); elapsed != 5*time.Second {
			t.Fatalf("wait took %s, want one 5s budget across all processes", elapsed)
		}
	})
}
