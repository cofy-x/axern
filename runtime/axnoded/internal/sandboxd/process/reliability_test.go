package process

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

func TestOutputOverflowDoesNotBlockCleanup(t *testing.T) {
	h := newOutputHub(2)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, err := h.subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		h.publish(StreamEvent{Stdout: []byte("output")})
	}
	h.close()
	found := false
	for event := range ch {
		found = found || event.Error != ""
	}
	if !found {
		t.Fatal("overflow was silently discarded")
	}
}

func TestOutputSubscriberLimit(t *testing.T) {
	h := newOutputHub(2)
	defer h.close()
	for i := 0; i < 64; i++ {
		if _, err := h.subscribe(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := h.subscribe(context.Background()); !errors.Is(err, ErrResourceLimit) {
		t.Fatalf("limit: %v", err)
	}
}

type blockingStdin struct {
	entered, closed chan struct{}
	once            sync.Once
}

func (b *blockingStdin) Write([]byte) (int, error) {
	close(b.entered)
	<-b.closed
	return 0, io.ErrClosedPipe
}
func (b *blockingStdin) Close() error { b.once.Do(func() { close(b.closed) }); return nil }

func TestCloseStdinInterruptsBlockedWrite(t *testing.T) {
	b := &blockingStdin{entered: make(chan struct{}), closed: make(chan struct{})}
	p := &managedProcess{stdin: b}
	done := make(chan error, 1)
	go func() { done <- p.writeStdin([]byte("data")) }()
	<-b.entered
	if err := p.closeStdin(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write: %v", err)
	}
	if err := p.closeStdin(); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentStartAndShutdownClosesAdmission(t *testing.T) {
	w := proc.NewWaiter(context.Background())
	defer w.Stop()
	r := NewRegistry(w, nil, "")
	gate := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-gate
			_, _ = r.Start(StartRequest{Args: []string{"/bin/sh", "-c", "read line"}, OpenStdin: true})
		}()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { <-gate; errs <- r.Shutdown(ctx, time.Second) }()
	}
	close(gate)
	wg.Wait()
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if _, err := r.Start(StartRequest{Args: []string{"/bin/true"}}); err == nil {
		t.Fatal("accepted after shutdown")
	}
	for _, p := range r.List().Processes {
		if p.State == ProcessStateRunning || p.State == ProcessStateStarting {
			t.Fatalf("survived shutdown: %+v", p)
		}
	}
}

func TestLateTimeoutAndSignalDoNotChangeCompletedResult(t *testing.T) {
	w := proc.NewWaiter(context.Background())
	defer w.Stop()
	r := NewRegistry(w, nil, "")
	s, err := r.Start(StartRequest{Args: []string{"/bin/sh", "-c", "exit 23"}})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := r.Wait(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	p, _ := r.process(s.ID)
	p.killAfter(0)
	if _, _, err := r.Signal(s.ID, mustSignal(t, "KILL")); err != nil {
		t.Fatal(err)
	}
	result, _ := r.Status(s.ID)
	if result.ExitCode == nil || *result.ExitCode != 23 || result.LastError != "" {
		t.Fatalf("late mutation: %+v", result)
	}
}
