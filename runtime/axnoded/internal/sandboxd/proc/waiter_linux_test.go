//go:build linux

package proc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestWaiterRegistersImmediateExitAndRejectsLateSignal(t *testing.T) {
	w := NewWaiter(context.Background())
	defer w.Stop()
	for i := 0; i < 50; i++ {
		cmd := exec.Command("/bin/sh", "-c", "exit 23")
		cmd.SysProcAttr = SysProcAttr()
		done, err := w.Start(cmd, cmd.Start)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case result := <-done:
			if result.ExitCode != 23 || result.Err != nil {
				t.Fatalf("result = %+v", result)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("exit was lost")
		}
		if err := w.Signal(cmd, os.Kill); !errors.Is(err, os.ErrProcessDone) {
			t.Fatalf("late signal = %v", err)
		}
	}
}

func TestWaiterRejectsUnownedCommandEvenWithSamePID(t *testing.T) {
	w := NewWaiter(context.Background())
	defer w.Stop()
	cmd := exec.Command("/bin/sh", "-c", "read line")
	cmd.SysProcAttr = SysProcAttr()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	defer stdin.Close()
	pid := 0
	done, err := w.Start(cmd, func() error {
		if err := cmd.Start(); err != nil {
			return err
		}
		pid = cmd.Process.Pid
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = w.Signal(cmd, os.Kill); <-done }()
	other := &exec.Cmd{Process: &os.Process{Pid: pid}}
	if err := w.Signal(other, os.Kill); !errors.Is(err, os.ErrProcessDone) {
		t.Fatalf("unowned signal = %v", err)
	}
}

func TestWaiterStopRejectsStart(t *testing.T) {
	w := NewWaiter(context.Background())
	w.Stop()
	cmd := exec.Command("/bin/true")
	_, err := w.Start(cmd, func() error { t.Fatal("started after stop"); return nil })
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("start = %v", err)
	}
}
