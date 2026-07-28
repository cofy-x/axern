package sshapi

import (
	"context"
	"io"
	"sync/atomic"
	"time"

	term "github.com/cofy-x/axern/gateway/gatewayd/internal/application/terminal"
	gossh "golang.org/x/crypto/ssh"
)

func (s *Server) copyInput(channel gossh.Channel, session *term.Session, touch func()) {
	buf := make([]byte, 32*1024)
	for {
		n, err := channel.Read(buf)
		if n > 0 {
			touch()
			if writeErr := session.Write(buf[:n]); writeErr != nil {
				return
			}
		}
		if err != nil {
			_ = session.CloseStdin()
			return
		}
	}
}

func (s *Server) copyOutput(channel gossh.Channel, session *term.Session, touch func(), closeDone func()) {
	defer closeDone()
	for {
		out, err := session.Recv()
		if err != nil {
			_, _ = io.WriteString(channel.Stderr(), err.Error()+"\n")
			return
		}
		touch()
		switch {
		case len(out.Stdout) > 0:
			if _, err := channel.Write(out.Stdout); err != nil {
				return
			}
		case len(out.Stderr) > 0:
			if _, err := channel.Stderr().Write(out.Stderr); err != nil {
				return
			}
		case out.Exit != nil:
			sendExitStatus(channel, out.Exit.Code)
			return
		}
	}
}

func (s *Server) watchIdle(ctx context.Context, channel gossh.Channel, idleTimeout time.Duration, lastActivity *atomic.Int64, closeDone func()) {
	if idleTimeout <= 0 {
		return
	}
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			last := time.Unix(0, lastActivity.Load())
			if time.Since(last) > idleTimeout {
				_, _ = io.WriteString(channel.Stderr(), "terminal idle timeout\n")
				_ = channel.Close()
				closeDone()
				return
			}
		}
	}
}

func sendExitStatus(channel gossh.Channel, code int32) {
	if code < 0 {
		code = 255
	}
	_, _ = channel.SendRequest("exit-status", false, gossh.Marshal(struct {
		Status uint32
	}{Status: uint32(code)}))
}
