package sshapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	term "github.com/cofy-x/axern/gateway/gatewayd/internal/application/terminal"
	"github.com/cofy-x/axern/gateway/gatewayd/internal/observability"
	sdkobs "github.com/cofy-x/axern/lib/go/observability"
	"go.opentelemetry.io/otel/attribute"
	gossh "golang.org/x/crypto/ssh"
)

type Server struct {
	address string
	config  *gossh.ServerConfig
	manager TerminalManager
	metrics *observability.Metrics
	obs     *sdkobs.Handle

	listener net.Listener
	mu       sync.Mutex
	wg       sync.WaitGroup
}

type TerminalManager interface {
	Options() term.Options
	OpenWithOptions(ctx context.Context, allocationID string, opts term.OpenOptions) (*term.Session, error)
}

func New(address, hostKeyPath, authorizedKeysPath string, manager TerminalManager, metrics *observability.Metrics, obs *sdkobs.Handle) (*Server, error) {
	hostKey, err := LoadHostKey(hostKeyPath)
	if err != nil {
		return nil, err
	}
	authorized, err := LoadAuthorizedKeys(authorizedKeysPath)
	if err != nil {
		return nil, err
	}
	cfg := &gossh.ServerConfig{
		NoClientAuth: false,
		PublicKeyCallback: func(conn gossh.ConnMetadata, key gossh.PublicKey) (*gossh.Permissions, error) {
			if strings.TrimSpace(conn.User()) == "" {
				return nil, fmt.Errorf("allocation id is required as ssh username")
			}
			if !authorized.Contains(key) {
				return nil, fmt.Errorf("unauthorized ssh public key")
			}
			return nil, nil
		},
		ServerVersion: "SSH-2.0-axern-gatewayd",
	}
	cfg.AddHostKey(hostKey)
	return &Server{
		address: strings.TrimSpace(address),
		config:  cfg,
		manager: manager,
		metrics: metrics,
		obs:     obs,
	}, nil
}

func (s *Server) handleSession(parent context.Context, allocationID string, channel gossh.Channel, requests <-chan *gossh.Request) {
	defer channel.Close()
	allocationID = strings.TrimSpace(allocationID)
	parent, op := s.obs.StartOperation(parent, sdkobs.OperationConfig{
		Name:        observability.SpanSSHSession,
		SpanAttrs:   []attribute.KeyValue{attribute.String(sdkobs.AttrAllocationID, allocationID)},
		MetricAttrs: []attribute.KeyValue{attribute.String(sdkobs.AttrOperation, "ssh_session")},
		Counter:     observability.MetricSSHSessionTotal,
		Duration:    observability.MetricSSHSessionDuration,
	})
	var opErr error
	defer func() { op.End(opErr) }()
	if allocationID == "" {
		_, _ = io.WriteString(channel.Stderr(), "allocation id is required as ssh username\n")
		op.SetErrorStatus("missing allocation id")
		opErr = errors.New("missing allocation id")
		return
	}

	opts := s.manager.Options()
	ctx, cancel := context.WithTimeout(parent, opts.MaxDuration)
	defer cancel()
	if s.metrics != nil {
		defer s.metrics.IncActiveTerminal()()
		s.metrics.TerminalEvent("ssh_open")
	}

	var initialCols, initialRows uint32
	termName := defaultTerminalName
	argv := []string{"/bin/sh"}
	containerUser := ""
	tty := true
	for {
		select {
		case <-ctx.Done():
			_, _ = io.WriteString(channel.Stderr(), ctx.Err().Error()+"\n")
			op.SetResult(sdkobs.ResultTimeout)
			opErr = ctx.Err()
			if s.metrics != nil {
				s.metrics.TerminalEvent("ssh_timeout")
			}
			return
		case req, ok := <-requests:
			if !ok {
				return
			}
			if isUnsupportedSessionRequest(req.Type) {
				reply(req, false)
				_, _ = io.WriteString(channel.Stderr(), req.Type+" is not supported by gatewayd ssh terminal\n")
				op.SetErrorStatus("unsupported ssh request")
				opErr = fmt.Errorf("unsupported ssh request: %s", req.Type)
				return
			}
			switch req.Type {
			case "env":
				name, value, ok := parseEnvRequest(req.Payload)
				if ok && name == execUserEnvName && validContainerUser(value) {
					containerUser = strings.TrimSpace(value)
					reply(req, true)
				} else {
					reply(req, false)
				}
			case "pty-req":
				pty, ok := parsePTYRequest(req.Payload)
				if ok {
					initialCols, initialRows = pty.Cols, pty.Rows
					termName = portableTerminalName(pty.Term)
				}
				reply(req, ok)
			case "window-change":
				cols, rows, ok := parseWindowChange(req.Payload)
				if ok {
					initialCols, initialRows = cols, rows
				}
			case "shell":
				reply(req, true)
				goto openSession
			case "exec":
				nextArgv, ok := parseExecRequest(req.Payload)
				if !ok {
					reply(req, false)
					_, _ = io.WriteString(channel.Stderr(), "invalid ssh exec request\n")
					op.SetErrorStatus("unsupported ssh exec request")
					opErr = errors.New("unsupported ssh exec request")
					return
				}
				argv = nextArgv
				tty = initialCols > 0 && initialRows > 0
				reply(req, true)
				goto openSession
			default:
				reply(req, false)
			}
		}
	}

openSession:
	session, err := s.manager.OpenWithOptions(ctx, allocationID, term.OpenOptions{
		Argv: argv,
		Env: map[string]string{
			"TERM": termName,
		},
		User: containerUser,
		TTY:  tty,
	})
	if err != nil {
		_, _ = io.WriteString(channel.Stderr(), "terminal target unavailable: "+err.Error()+"\n")
		op.SetErrorStatus("terminal target unavailable")
		opErr = err
		if s.metrics != nil {
			s.metrics.TerminalEvent("ssh_error")
		}
		return
	}
	defer session.Close()
	if initialCols > 0 && initialRows > 0 {
		_ = session.Resize(initialCols, initialRows)
	}

	var lastActivity atomic.Int64
	touch := func() { lastActivity.Store(time.Now().UnixNano()) }
	touch()
	done := make(chan struct{})
	var doneOnce sync.Once
	closeDone := func() { doneOnce.Do(func() { close(done) }) }
	go s.watchIdle(ctx, channel, opts.IdleTimeout, &lastActivity, closeDone)
	go s.copyInput(channel, session, touch)
	go s.copyOutput(channel, session, touch, closeDone)

	for {
		select {
		case <-ctx.Done():
			_, _ = io.WriteString(channel.Stderr(), ctx.Err().Error()+"\n")
			op.SetResult(sdkobs.ResultTimeout)
			opErr = ctx.Err()
			if s.metrics != nil {
				s.metrics.TerminalEvent("ssh_timeout")
			}
			return
		case req, ok := <-requests:
			if !ok {
				_ = session.CloseStdin()
				return
			}
			touch()
			if isUnsupportedSessionRequest(req.Type) {
				reply(req, false)
				_, _ = io.WriteString(channel.Stderr(), req.Type+" is not supported by gatewayd ssh terminal\n")
				op.SetErrorStatus("unsupported ssh request")
				opErr = fmt.Errorf("unsupported ssh request: %s", req.Type)
				return
			}
			switch req.Type {
			case "window-change":
				cols, rows, ok := parseWindowChange(req.Payload)
				if ok {
					_ = session.Resize(cols, rows)
				}
			default:
				reply(req, false)
			}
		case <-done:
			if s.metrics != nil {
				s.metrics.TerminalEvent("ssh_exit")
			}
			op.SetResult(sdkobs.ResultExit)
			return
		}
	}
}

func reply(req *gossh.Request, ok bool) {
	if req.WantReply {
		_ = req.Reply(ok, nil)
	}
}
