package tunnel

import (
	"context"
	"net"
	"time"

	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
)

func (c *connector) handleFrame(ctx context.Context, frame *tunnelv1.TunnelFrame) {
	switch payload := frame.GetPayload().(type) {
	case *tunnelv1.TunnelFrame_Ping:
		_ = c.send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_Pong{Pong: &tunnelv1.Pong{ID: payload.Ping.GetID()}}})
	case *tunnelv1.TunnelFrame_Pong:
		return
	case *tunnelv1.TunnelFrame_StreamOpen:
		c.openLocal(ctx, payload.StreamOpen.GetStreamID())
	case *tunnelv1.TunnelFrame_StreamData:
		c.mu.Lock()
		conn := c.conns[payload.StreamData.GetStreamID()]
		c.mu.Unlock()
		if conn != nil && len(payload.StreamData.GetData()) > 0 {
			_, _ = conn.Write(payload.StreamData.GetData())
		}
	case *tunnelv1.TunnelFrame_StreamClose:
		c.closeLocal(payload.StreamClose.GetStreamID())
	}
}

func (c *connector) openLocal(ctx context.Context, streamID uint64) {
	if c.config.MaxStreams > 0 {
		c.mu.Lock()
		active := len(c.conns)
		c.mu.Unlock()
		if active >= c.config.MaxStreams {
			_ = c.send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamClose{StreamClose: &tunnelv1.StreamClose{StreamID: streamID, Error: "connector max streams reached"}}})
			return
		}
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", c.localTarget)
	if err != nil {
		_ = c.send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamClose{StreamClose: &tunnelv1.StreamClose{StreamID: streamID, Error: err.Error()}}})
		return
	}
	var old net.Conn
	c.mu.Lock()
	old = c.conns[streamID]
	c.conns[streamID] = conn
	c.mu.Unlock()
	if old != nil {
		_ = old.Close()
	}
	go c.copyLocalToRelay(streamID, conn)
}

func (c *connector) copyLocalToRelay(streamID uint64, conn net.Conn) {
	defer c.closeLocalConn(streamID, conn)
	buf := make([]byte, 32*1024)
	for {
		n, err := conn.Read(buf)
		if n > 0 {
			frame := &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamData{StreamData: &tunnelv1.StreamData{
				StreamID: streamID,
				Data:     append([]byte(nil), buf[:n]...),
			}}}
			if sendErr := c.send(frame); sendErr != nil {
				return
			}
		}
		if err != nil {
			_ = c.send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamClose{StreamClose: &tunnelv1.StreamClose{StreamID: streamID}}})
			return
		}
	}
}

func (c *connector) closeLocal(streamID uint64) {
	c.mu.Lock()
	conn := c.conns[streamID]
	delete(c.conns, streamID)
	c.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (c *connector) closeLocalConn(streamID uint64, conn net.Conn) {
	c.mu.Lock()
	if c.conns[streamID] == conn {
		delete(c.conns, streamID)
	}
	c.mu.Unlock()
	_ = conn.Close()
}

func (c *connector) closeAll() {
	c.mu.Lock()
	conns := make([]net.Conn, 0, len(c.conns))
	for streamID, conn := range c.conns {
		delete(c.conns, streamID)
		conns = append(conns, conn)
	}
	c.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
	}
}
