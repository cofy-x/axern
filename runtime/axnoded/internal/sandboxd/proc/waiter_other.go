//go:build !linux

package proc

import (
	"context"
	"os"
	"os/exec"
)

type Waiter struct{}

func NewWaiter(context.Context) *Waiter {
	return &Waiter{}
}

func (w *Waiter) Start(cmd *exec.Cmd, start func() error) (<-chan Result, error) {
	if err := start(); err != nil {
		return nil, err
	}
	ch := make(chan Result, 1)
	go func() {
		err := cmd.Wait()
		result := ResultFromError(err)
		ch <- result
		close(ch)
	}()
	return ch, nil
}

// Non-Linux support is host tooling only. Use the OS handle for the direct
// child; unlike Linux PID 1 we do not own group reaping here.
func (w *Waiter) Signal(cmd *exec.Cmd, signal os.Signal) error {
	return cmd.Process.Signal(signal)
}

func (w *Waiter) Stop() {}
