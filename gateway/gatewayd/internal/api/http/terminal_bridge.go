package httpapi

import (
	"context"
	"net"
	"sync"
	"time"

	term "github.com/cofy-x/axern/gateway/gatewayd/internal/application/terminal"
	gatewayv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/gateway/v1"
	"github.com/gorilla/websocket"
)

func (t *Terminal) bridge(ctx context.Context, ws *websocket.Conn, resolved *gatewayv1.ResolveAllocationTerminalResponse, opts term.OpenOptions) {
	socket := terminalSocket{conn: ws, writeTimeout: t.options.WriteTimeout}
	session, err := t.manager.OpenResolvedWithOptions(ctx, resolved, opts)
	if err != nil {
		_ = socket.writeJSON(map[string]string{"type": "error", "message": err.Error()})
		return
	}
	defer session.Close()

	done := make(chan struct{})
	go t.copyTerminalOutput(&socket, session, done)

	ws.SetReadLimit(t.options.MaxMessageBytes)
	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			socket.writeJSON(map[string]string{"type": "error", "message": ctx.Err().Error()})
			if t.metrics != nil {
				t.metrics.TerminalEvent("timeout")
			}
			return
		default:
		}
		_ = ws.SetReadDeadline(t.nextReadDeadline(ctx))
		messageType, data, err := ws.ReadMessage()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				_ = socket.writeJSON(map[string]string{"type": "error", "message": "terminal idle timeout"})
				if t.metrics != nil {
					t.metrics.TerminalEvent("timeout")
				}
			}
			_ = session.CloseStdin()
			_ = session.Close()
			return
		}
		if messageType == websocket.TextMessage {
			if handled := t.handleTerminalTextMessage(&socket, session, data); handled {
				continue
			}
			if msg, ok := parseTerminalClientMessage(data); ok && msg.Type == "stdin" {
				data = msg.Data
			}
		}
		if err := session.Write(data); err != nil {
			return
		}
	}
}

type terminalSocket struct {
	conn         *websocket.Conn
	writeTimeout time.Duration
	writeMu      sync.Mutex
}

func (s *terminalSocket) writeJSON(value any) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(s.writeTimeout))
	return s.conn.WriteJSON(value)
}

func (t *Terminal) copyTerminalOutput(socket *terminalSocket, session *term.Session, done chan<- struct{}) {
	defer close(done)
	for {
		out, err := session.Recv()
		if err != nil {
			socket.writeJSON(map[string]string{"type": "error", "message": err.Error()})
			if t.metrics != nil {
				t.metrics.TerminalEvent("error")
			}
			return
		}
		switch {
		case len(out.Stdout) > 0:
			if err := socket.writeJSON(map[string]any{"type": "stdout", "data": string(out.Stdout)}); err != nil {
				return
			}
		case len(out.Stderr) > 0:
			if err := socket.writeJSON(map[string]any{"type": "stderr", "data": string(out.Stderr)}); err != nil {
				return
			}
		case out.Exit != nil:
			_ = socket.writeJSON(map[string]any{"type": "exit", "exit_code": out.Exit.Code, "message": out.Exit.Message})
			if t.metrics != nil {
				t.metrics.TerminalEvent("exit")
			}
			return
		}
	}
}

func (t *Terminal) handleTerminalTextMessage(socket *terminalSocket, session *term.Session, data []byte) bool {
	if cols, rows, ok := parseResizeMessage(data); ok {
		_ = session.Resize(cols, rows)
		return true
	}
	msg, ok := parseTerminalClientMessage(data)
	if !ok {
		return false
	}
	switch msg.Type {
	case "ping":
		_ = socket.writeJSON(map[string]string{"type": "pong"})
		return true
	case "close_stdin":
		_ = session.CloseStdin()
		return true
	default:
		return false
	}
}
