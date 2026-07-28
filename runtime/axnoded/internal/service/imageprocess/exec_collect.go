package imageprocess

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

type ExecResult struct {
	Exit            contract.Exit
	Stdout          []byte
	Stderr          []byte
	StdoutTruncated bool
	StderrTruncated bool
}

func CollectExec(ctx context.Context, session contract.Session) (ExecResult, error) {
	outputDone := make(chan struct {
		stdout          []byte
		stderr          []byte
		stdoutTruncated bool
		stderrTruncated bool
		err             error
	}, 1)
	go func() {
		var stdout, stderr limitedBuffer
		for {
			chunk, recvErr := session.Recv()
			if recvErr != nil {
				if errors.Is(recvErr, io.EOF) {
					outputDone <- struct {
						stdout          []byte
						stderr          []byte
						stdoutTruncated bool
						stderrTruncated bool
						err             error
					}{stdout: stdout.Bytes(), stderr: stderr.Bytes(), stdoutTruncated: stdout.Truncated(), stderrTruncated: stderr.Truncated()}
					return
				}
				outputDone <- struct {
					stdout          []byte
					stderr          []byte
					stdoutTruncated bool
					stderrTruncated bool
					err             error
				}{err: recvErr}
				return
			}
			stdout.Write(chunk.Stdout)
			stderr.Write(chunk.Stderr)
		}
	}()

	waitCh := make(chan struct {
		exit contract.Exit
		err  error
	}, 1)
	go func() {
		exit, waitErr := session.Wait()
		waitCh <- struct {
			exit contract.Exit
			err  error
		}{exit: exit, err: waitErr}
	}()

	var output struct {
		stdout          []byte
		stderr          []byte
		stdoutTruncated bool
		stderrTruncated bool
		err             error
	}
	select {
	case output = <-outputDone:
	case <-ctx.Done():
		return ExecResult{}, ctx.Err()
	}
	if output.err != nil {
		return ExecResult{}, output.err
	}

	select {
	case result := <-waitCh:
		if result.err != nil && !contract.IsExitStatusUnavailable(result.err) {
			return ExecResult{}, result.err
		}
		return ExecResult{
			Exit:            result.exit,
			Stdout:          output.stdout,
			Stderr:          output.stderr,
			StdoutTruncated: output.stdoutTruncated,
			StderrTruncated: output.stderrTruncated,
		}, nil
	case <-ctx.Done():
		return ExecResult{}, ctx.Err()
	}
}

type limitedBuffer struct {
	buf       bytes.Buffer
	truncated bool
}

func (b *limitedBuffer) Write(data []byte) {
	if len(data) == 0 {
		return
	}
	remaining := outputLimit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return
	}
	if len(data) > remaining {
		b.buf.Write(data[:remaining])
		b.truncated = true
		return
	}
	b.buf.Write(data)
}

func (b *limitedBuffer) Bytes() []byte {
	return append([]byte(nil), b.buf.Bytes()...)
}

func (b *limitedBuffer) Truncated() bool {
	return b.truncated
}
