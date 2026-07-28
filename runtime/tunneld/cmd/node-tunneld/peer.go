package main

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
)

type nodePeer struct {
	stream   tunnelv1.TunnelRelay_ConnectPeerClient
	listener net.Listener
	writeMu  sync.Mutex
	nextID   atomic.Uint64
	mu       sync.Mutex
	conns    map[uint64]net.Conn
}

func (p *nodePeer) run(ctx context.Context) error {
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
		return err
	}
}

func (p *nodePeer) acceptLoop() error {
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

func (p *nodePeer) recvLoop() error {
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

func (p *nodePeer) copyConnToRelay(streamID uint64, conn net.Conn) {
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

func (p *nodePeer) closeConn(streamID uint64) {
	p.mu.Lock()
	conn := p.conns[streamID]
	delete(p.conns, streamID)
	p.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (p *nodePeer) closeConnFor(streamID uint64, conn net.Conn) {
	p.mu.Lock()
	if p.conns[streamID] == conn {
		delete(p.conns, streamID)
	}
	p.mu.Unlock()
	_ = conn.Close()
}

func (p *nodePeer) closeAll() {
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

func (p *nodePeer) send(frame *tunnelv1.TunnelFrame) error {
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
