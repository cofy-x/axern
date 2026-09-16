package process

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

type managedProcess struct {
	waiter        *proc.Waiter
	status        Status
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	stdout        *limitedBuffer
	stderr        *limitedBuffer
	stdoutPipe    io.ReadCloser
	stderrPipe    io.ReadCloser
	captureOutput bool
	streamOutput  bool
	terminal      bool
	outputs       *outputHub
	tty           *os.File
	outputDone    chan struct{}
	done          chan struct{}
	mu            sync.RWMutex
	stdinMu       sync.Mutex
	writeMu       sync.Mutex
}

func (p *managedProcess) configureOutput(cmd *exec.Cmd, request StartRequest) error {
	if !request.CaptureOutput && !request.StreamOutput {
		return nil
	}
	p.captureOutput = request.CaptureOutput
	p.streamOutput = request.StreamOutput
	if request.CaptureOutput {
		p.stdout = newLimitedBuffer(maxCapturedOutputBytes)
		p.stderr = newLimitedBuffer(maxCapturedOutputBytes)
	}
	if request.StreamOutput {
		p.outputs = newOutputHub(maxStreamBacklogEvents)
	}
	if request.Terminal {
		return nil
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	p.stdoutPipe = stdoutPipe
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	p.stderrPipe = stderrPipe
	return nil
}

func (p *managedProcess) writeStdin(data []byte) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	p.stdinMu.Lock()
	stdin := p.stdin
	p.stdinMu.Unlock()
	if stdin == nil {
		return fmt.Errorf("process stdin is not open")
	}
	return writeAll(stdin, data)
}

func (p *managedProcess) closeStdin() error {
	p.stdinMu.Lock()
	if p.terminal {
		p.stdinMu.Unlock()
		return nil
	}
	if p.stdin == nil {
		p.stdinMu.Unlock()
		return nil
	}
	stdin := p.stdin
	p.stdin = nil
	p.stdinMu.Unlock()
	return stdin.Close()
}

func (p *managedProcess) closeIO() {
	_ = p.closeStdin()
	if p.stdoutPipe != nil {
		_ = p.stdoutPipe.Close()
	}
	if p.stderrPipe != nil {
		_ = p.stderrPipe.Close()
	}
	if p.tty != nil {
		p.mu.Lock()
		_ = p.tty.Close()
		p.mu.Unlock()
	}
}

func (p *managedProcess) snapshot() Status {
	p.mu.RLock()
	defer p.mu.RUnlock()
	status := p.status
	if p.captureOutput {
		if p.stdout != nil {
			status.Stdout = p.stdout.String()
			status.StdoutTruncated = p.stdout.Truncated()
		}
		if p.stderr != nil {
			status.Stderr = p.stderr.String()
			status.StderrTruncated = p.stderr.Truncated()
		}
	}
	return status
}

func (p *managedProcess) active() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.status.State == ProcessStateStarting || p.status.State == ProcessStateRunning
}

func writeAll(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := writer.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
