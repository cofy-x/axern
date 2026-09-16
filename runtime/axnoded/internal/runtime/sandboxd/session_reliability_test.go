package sandboxd

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
)

type blockedOutputClient struct {
	fakeSessionClient
	full     chan struct{}
	finished chan struct{}
}

type canceledAccessClient struct {
	fakeSessionClient
	killed atomic.Bool
}

func (c *canceledAccessClient) WaitProcess(ctx context.Context, _ string) (ProcessStatus, error) {
	if err := ctx.Err(); err != nil {
		return ProcessStatus{}, err
	}
	if c.killed.Load() {
		return ProcessStatus{State: "exited", ExitCode: sessionIntPtr(137)}, nil
	}
	return ProcessStatus{}, errors.New("runtime exit not confirmed")
}

func (c *canceledAccessClient) SignalProcess(_ context.Context, _ string, signal string) (ProcessStatus, error) {
	if signal == "KILL" {
		c.killed.Store(true)
	}
	return ProcessStatus{}, nil
}

func TestSessionCloseDoesNotTreatCanceledWaitAsProcessExit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client := &canceledAccessClient{}
	s := NewSession(ctx, client, "proc")
	if _, err := s.Wait(); !errors.Is(err, context.Canceled) {
		t.Fatalf("access wait: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if !client.killed.Load() {
		t.Fatal("canceled access wait incorrectly confirmed runtime exit")
	}
}

func (c *blockedOutputClient) StreamProcess(ctx context.Context, _ string, emit func(ProcessStreamEvent) error) error {
	defer close(c.finished)
	for i := 0; i < 8; i++ {
		if err := emit(ProcessStreamEvent{Stdout: []byte("data")}); err != nil {
			return err
		}
	}
	close(c.full)
	return emit(ProcessStreamEvent{Stdout: []byte("blocked")})
}

func (c *blockedOutputClient) WaitProcess(ctx context.Context, _ string) (ProcessStatus, error) {
	<-ctx.Done()
	return ProcessStatus{}, ctx.Err()
}

func TestSessionCancellationUnblocksUnconsumedOutput(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := &blockedOutputClient{full: make(chan struct{}), finished: make(chan struct{})}
	s := NewSession(ctx, client, "proc")
	<-client.full
	cancel()
	if _, err := s.Wait(); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait: %v", err)
	}
	<-client.finished
	for {
		if _, err := s.Recv(); err != nil {
			break
		}
	}
}
