package process

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
)

func (p *managedProcess) start(cmd *exec.Cmd, request StartRequest, user processUser, hasUser bool) error {
	if !request.Terminal {
		return startProcessGroupWithUser(cmd, user, hasUser)
	}
	size, err := initialTerminalSize(request.InitialCols, request.InitialRows)
	if err != nil {
		return err
	}
	tty, err := startTerminalProcess(cmd, size, user, hasUser)
	if err != nil {
		return err
	}
	p.tty = tty
	p.stdin = tty
	p.outputDone = make(chan struct{})
	if request.Stdin != "" {
		if err := writeAll(tty, []byte(request.Stdin)); err != nil {
			_ = tty.Close()
			return err
		}
	}
	go p.copyTerminalOutput(tty)
	return nil
}

func initialTerminalSize(cols uint32, rows uint32) (*pty.Winsize, error) {
	if cols == 0 && rows == 0 {
		return nil, nil
	}
	size, err := terminalSize(cols, rows)
	if err != nil {
		return nil, fmt.Errorf("invalid initial terminal size: %w", err)
	}
	return size, nil
}

func (p *managedProcess) copyTerminalOutput(tty *os.File) {
	defer close(p.outputDone)
	writer := p.outputWriter(StreamEvent{Stdout: []byte{}})
	if writer == nil {
		_, _ = io.Copy(io.Discard, tty)
		return
	}
	buf := make([]byte, 32*1024)
	for {
		n, err := tty.Read(buf)
		if n > 0 {
			_, _ = writer.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func (p *managedProcess) startPipeOutputCopy() {
	if p.stdoutPipe == nil && p.stderrPipe == nil {
		return
	}
	p.outputDone = make(chan struct{})
	go func() {
		defer close(p.outputDone)
		var wg sync.WaitGroup
		if p.stdoutPipe != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = io.Copy(p.outputWriter(StreamEvent{Stdout: []byte{}}), p.stdoutPipe)
			}()
		}
		if p.stderrPipe != nil {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = io.Copy(p.outputWriter(StreamEvent{Stderr: []byte{}}), p.stderrPipe)
			}()
		}
		wg.Wait()
	}()
}

func (p *managedProcess) outputWriter(template StreamEvent) io.Writer {
	var writer io.Writer
	if p.captureOutput && template.Stdout != nil && p.stdout != nil {
		writer = p.stdout
	} else if p.captureOutput && template.Stderr != nil && p.stderr != nil {
		writer = p.stderr
	}
	if p.streamOutput && p.outputs != nil {
		stream := streamWriter{emit: func(data []byte) {
			event := template
			if template.Stdout != nil {
				event.Stdout = data
			}
			if template.Stderr != nil {
				event.Stderr = data
			}
			p.outputs.publish(event)
		}}
		if writer != nil {
			writer = io.MultiWriter(writer, stream)
		} else {
			writer = stream
		}
	}
	return writer
}

func (p *managedProcess) waitForOutput() {
	if p.outputDone != nil {
		select {
		case <-p.outputDone:
		case <-time.After(time.Second):
		}
	}
	if p.tty != nil {
		_ = p.tty.Close()
	}
}

func (p *managedProcess) resize(cols uint32, rows uint32) error {
	p.mu.RLock()
	tty := p.tty
	terminal := p.terminal
	state := p.status.State
	p.mu.RUnlock()
	if !terminal || tty == nil {
		return fmt.Errorf("process terminal is not open")
	}
	if state != ProcessStateRunning {
		return fmt.Errorf("process is not running")
	}
	size, err := terminalSize(cols, rows)
	if err != nil {
		return fmt.Errorf("invalid terminal resize: %w", err)
	}
	return pty.Setsize(tty, size)
}

func terminalSize(cols uint32, rows uint32) (*pty.Winsize, error) {
	if cols == 0 || rows == 0 {
		return nil, fmt.Errorf("cols and rows must be positive")
	}
	if cols > maxTerminalDimension || rows > maxTerminalDimension {
		return nil, fmt.Errorf("cols and rows must be <= %d", maxTerminalDimension)
	}
	return &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	}, nil
}
