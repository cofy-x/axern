package process

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

type managedProcess struct {
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
	managedProxy  *managedProxySession
	outputDone    chan struct{}
	done          chan struct{}
	mu            sync.RWMutex
	stdinMu       sync.Mutex
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
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	p.stdoutPipe = stdoutPipe
	p.stderrPipe = stderrPipe
	return nil
}

func (p *managedProcess) writeStdin(data []byte) error {
	p.stdinMu.Lock()
	defer p.stdinMu.Unlock()
	if p.stdin == nil {
		return fmt.Errorf("process stdin is not open")
	}
	return writeAll(p.stdin, data)
}

func (p *managedProcess) closeStdin() error {
	p.stdinMu.Lock()
	defer p.stdinMu.Unlock()
	if p.terminal {
		return nil
	}
	if p.stdin == nil {
		return nil
	}
	err := p.stdin.Close()
	p.stdin = nil
	return err
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
