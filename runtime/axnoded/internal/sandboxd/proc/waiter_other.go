//go:build !linux

package proc

import (
	"context"
	"os/exec"
)

type Waiter struct{}

func NewWaiter(context.Context) *Waiter {
	return &Waiter{}
}

func (w *Waiter) Watch(cmd *exec.Cmd) <-chan Result {
	ch := make(chan Result, 1)
	go func() {
		err := cmd.Wait()
		result := ResultFromError(err)
		ch <- result
		close(ch)
	}()
	return ch
}

func (w *Waiter) Stop() {}
