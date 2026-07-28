package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/cofy-x/axern/lib/go/grpcclient"
	"github.com/cofy-x/axern/runtime/tunneld/internal/relaytls"
	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "tunnel-agent: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		sessionID       string
		token           string
		edgeTarget      string
		listenHost      string
		listenPort      int
		relayCACert     string
		relayCAPEM      string
		relayServerName string
	)
	flag.StringVar(&sessionID, "session-id", "", "tunnel session id")
	flag.StringVar(&token, "token", "", "node peer token")
	flag.StringVar(&edgeTarget, "edge-target", "", "tunneld relay target")
	flag.StringVar(&listenHost, "listen-host", "127.0.0.1", "address to listen on inside the sandbox")
	flag.IntVar(&listenPort, "listen-port", 0, "port to listen on inside the sandbox")
	flag.StringVar(&relayCACert, "relay-tls-ca-cert", "", "CA certificate used to verify the tunnel relay")
	flag.StringVar(&relayCAPEM, "relay-tls-ca-pem", "", "CA certificate PEM content used to verify the tunnel relay")
	flag.StringVar(&relayServerName, "relay-server-name", "", "server name used to verify the tunnel relay certificate")
	flag.Parse()
	if sessionID == "" || token == "" || edgeTarget == "" || listenPort <= 0 || listenPort > 65535 {
		return fmt.Errorf("session-id, token, edge-target, and listen-port are required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	dialOpts, err := relaytls.DialOptions(relaytls.ClientConfig{CACert: relayCACert, CAPEM: relayCAPEM, ServerName: relayServerName})
	if err != nil {
		return err
	}
	conn, err := grpcclient.NewReadyClient(ctx, edgeTarget, dialOpts...)
	if err != nil {
		return err
	}
	defer conn.Close()
	stream, err := tunnelv1.NewTunnelRelayClient(conn).ConnectPeer(ctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_PeerOpen{PeerOpen: &tunnelv1.PeerOpen{
		SessionID: sessionID,
		PeerKind:  tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE,
		Token:     token,
	}}}); err != nil {
		return err
	}

	addr := net.JoinHostPort(listenHost, strconv.Itoa(listenPort))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	fmt.Fprintln(os.Stdout, "ready")

	peer := &agentPeer{stream: stream, listener: ln, conns: make(map[uint64]net.Conn)}
	peer.nextID.Store(initialStreamCounter())
	return peer.run(ctx)
}

type agentPeer struct {
	stream   tunnelv1.TunnelRelay_ConnectPeerClient
	listener net.Listener
	writeMu  sync.Mutex
	nextID   atomic.Uint64
	mu       sync.Mutex
	conns    map[uint64]net.Conn
}

func (p *agentPeer) run(ctx context.Context) error {
	defer p.closeAll()
	errCh := make(chan error, 2)
	go func() {
		<-ctx.Done()
		_ = p.listener.Close()
	}()
	go func() { errCh <- p.acceptLoop() }()
	go func() { errCh <- p.recvLoop() }()
	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		if errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
}

func (p *agentPeer) acceptLoop() error {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return err
		}
		streamID := p.nextID.Add(1)
		p.mu.Lock()
		p.conns[streamID] = conn
		p.mu.Unlock()
		if err := p.send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamOpen{StreamOpen: &tunnelv1.StreamOpen{StreamID: streamID}}}); err != nil {
			p.closeConnFor(streamID, conn)
			return err
		}
		go p.copyConnToRelay(streamID, conn)
	}
}

func (p *agentPeer) recvLoop() error {
	for {
		frame, err := p.stream.Recv()
		if err != nil {
			return err
		}
		switch payload := frame.GetPayload().(type) {
		case *tunnelv1.TunnelFrame_Ping:
			_ = p.send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_Pong{Pong: &tunnelv1.Pong{ID: payload.Ping.GetID()}}})
		case *tunnelv1.TunnelFrame_Pong:
			continue
		case *tunnelv1.TunnelFrame_StreamData:
			p.mu.Lock()
			conn := p.conns[payload.StreamData.GetStreamID()]
			p.mu.Unlock()
			if conn != nil && len(payload.StreamData.GetData()) > 0 {
				_, _ = conn.Write(payload.StreamData.GetData())
			}
		case *tunnelv1.TunnelFrame_StreamClose:
			p.closeConn(payload.StreamClose.GetStreamID())
		}
	}
}

func (p *agentPeer) copyConnToRelay(streamID uint64, conn net.Conn) {
	defer p.closeConnFor(streamID, conn)
	buf := make([]byte, 32*1024)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			if sendErr := p.send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamData{StreamData: &tunnelv1.StreamData{StreamID: streamID, Data: append([]byte(nil), buf[:n]...)}}}); sendErr != nil {
				return
			}
		}
		if err != nil {
			_ = p.send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamClose{StreamClose: &tunnelv1.StreamClose{StreamID: streamID}}})
			return
		}
	}
}

func (p *agentPeer) closeConn(streamID uint64) {
	p.mu.Lock()
	conn := p.conns[streamID]
	delete(p.conns, streamID)
	p.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (p *agentPeer) closeConnFor(streamID uint64, conn net.Conn) {
	p.mu.Lock()
	if p.conns[streamID] == conn {
		delete(p.conns, streamID)
	}
	p.mu.Unlock()
	_ = conn.Close()
}

func (p *agentPeer) closeAll() {
	p.mu.Lock()
	conns := make([]net.Conn, 0, len(p.conns))
	for streamID, conn := range p.conns {
		delete(p.conns, streamID)
		conns = append(conns, conn)
	}
	p.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
	}
}

func (p *agentPeer) send(frame *tunnelv1.TunnelFrame) error {
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	return p.stream.Send(frame)
}

func initialStreamCounter() uint64 {
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return uint64(binary.BigEndian.Uint32(raw[:])) << 32
	}
	return uint64(time.Now().UnixNano()) << 16
}
