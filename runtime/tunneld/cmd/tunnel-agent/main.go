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

	"github.com/cofy-x/axern/runtime/tunneld/internal/stdioframe"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
)

const (
	maxAgentStreams  = 128
	connWriteTimeout = 5 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "tunnel-agent: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var listenPort int
	flag.IntVar(&listenPort, "listen-port", 0, "port to listen on inside the sandbox")
	flag.Parse()
	if listenPort <= 0 || listenPort > 65535 {
		return fmt.Errorf("listen-port must be between 1 and 65535")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serve(ctx, listenPort, os.Stdin, os.Stdout)
}

func serve(ctx context.Context, listenPort int, input io.ReadCloser, output io.WriteCloser) error {
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(listenPort))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return serveListener(ctx, ln, input, output)
}

func serveListener(ctx context.Context, ln net.Listener, input io.ReadCloser, output io.WriteCloser) error {
	defer ln.Close()
	n, err := io.WriteString(output, stdioframe.ReadyLine)
	if err != nil {
		return err
	}
	if n != len(stdioframe.ReadyLine) {
		return io.ErrShortWrite
	}

	peer := &agentPeer{stream: stdioframe.New(input, output), listener: ln, conns: make(map[uint64]net.Conn)}
	peer.nextID.Store(initialStreamCounter())
	return peer.run(ctx, input, output)
}

type frameStream interface {
	Send(*tunnelv1.TunnelFrame) error
	Recv() (*tunnelv1.TunnelFrame, error)
}

type agentPeer struct {
	stream   frameStream
	listener net.Listener
	nextID   atomic.Uint64
	mu       sync.Mutex
	conns    map[uint64]net.Conn
	copyWG   sync.WaitGroup
}

func (p *agentPeer) run(ctx context.Context, input, output io.Closer) error {
	errCh := make(chan error, 2)
	var acceptWG, recvWG sync.WaitGroup
	acceptWG.Add(1)
	go func() {
		defer acceptWG.Done()
		errCh <- p.acceptLoop()
	}()
	recvWG.Add(1)
	go func() {
		defer recvWG.Done()
		errCh <- p.recvLoop()
	}()
	var err error
	cancelled := false
	select {
	case <-ctx.Done():
		cancelled = true
	case err = <-errCh:
	}
	_ = p.listener.Close()
	_ = input.Close()
	_ = output.Close()
	acceptWG.Wait()
	p.closeAll()
	recvWG.Wait()
	p.copyWG.Wait()
	if cancelled || errors.Is(err, net.ErrClosed) || errors.Is(err, io.EOF) {
		return nil
	}
	return err
}

func (p *agentPeer) acceptLoop() error {
	for {
		conn, err := p.listener.Accept()
		if err != nil {
			return err
		}
		streamID := p.nextID.Add(1)
		p.mu.Lock()
		if len(p.conns) >= maxAgentStreams {
			p.mu.Unlock()
			_ = conn.Close()
			continue
		}
		p.conns[streamID] = conn
		p.mu.Unlock()
		if err := p.send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamOpen{StreamOpen: &tunnelv1.StreamOpen{StreamID: streamID}}}); err != nil {
			p.closeConnFor(streamID, conn)
			return err
		}
		p.copyWG.Add(1)
		go func() {
			defer p.copyWG.Done()
			p.copyConnToRelay(streamID, conn)
		}()
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
			if err := p.send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_Pong{Pong: &tunnelv1.Pong{ID: payload.Ping.GetID()}}}); err != nil {
				return err
			}
		case *tunnelv1.TunnelFrame_Pong:
			continue
		case *tunnelv1.TunnelFrame_StreamData:
			p.mu.Lock()
			conn := p.conns[payload.StreamData.GetStreamID()]
			p.mu.Unlock()
			if conn != nil && len(payload.StreamData.GetData()) > 0 {
				if err := writeConn(conn, payload.StreamData.GetData()); err != nil {
					p.closeConnFor(payload.StreamData.GetStreamID(), conn)
				}
			}
		case *tunnelv1.TunnelFrame_StreamClose:
			p.closeConn(payload.StreamClose.GetStreamID())
		}
	}
}

func writeConn(conn net.Conn, data []byte) error {
	// Bound a single frame's total write time, including repeated short
	// writes. A non-reading sandbox process must not hold its stream forever.
	if err := conn.SetWriteDeadline(time.Now().Add(connWriteTimeout)); err != nil {
		return err
	}
	for len(data) > 0 {
		n, err := conn.Write(data)
		if err != nil {
			return err
		}
		if n <= 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
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
	return p.stream.Send(frame)
}

func initialStreamCounter() uint64 {
	var raw [4]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return uint64(binary.BigEndian.Uint32(raw[:])) << 32
	}
	return uint64(time.Now().UnixNano()) << 16
}
