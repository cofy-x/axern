package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/tunneld/internal/stdioframe"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
)

func TestAgentBridgesLoopbackWithoutRelayNetwork(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	guestInput, hostOutput := io.Pipe()
	hostInput, guestOutput := io.Pipe()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- serveListener(ctx, listener, guestInput, guestOutput) }()
	var ready [len(stdioframe.ReadyLine)]byte
	if _, err := io.ReadFull(hostInput, ready[:]); err != nil {
		t.Fatal(err)
	}
	if string(ready[:]) != stdioframe.ReadyLine {
		t.Fatalf("ready handshake = %q", ready)
	}
	host := stdioframe.New(hostInput, hostOutput)
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	opened, err := host.Recv()
	if err != nil {
		t.Fatal(err)
	}
	streamID := opened.GetStreamOpen().GetStreamID()
	if streamID == 0 {
		t.Fatalf("first frame = %v, want stream open", opened)
	}
	if _, err := conn.Write([]byte("from-allocation")); err != nil {
		t.Fatal(err)
	}
	data, err := host.Recv()
	if err != nil {
		t.Fatal(err)
	}
	if data.GetStreamData().GetStreamID() != streamID || string(data.GetStreamData().GetData()) != "from-allocation" {
		t.Fatalf("upstream frame = %v", data)
	}
	if err := host.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamData{
		StreamData: &tunnelv1.StreamData{StreamID: streamID, Data: []byte("from-caller")},
	}}); err != nil {
		t.Fatal(err)
	}
	var response [len("from-caller")]byte
	if _, err := io.ReadFull(conn, response[:]); err != nil {
		t.Fatal(err)
	}
	if string(response[:]) != "from-caller" {
		t.Fatalf("downstream bytes = %q", response)
	}
	// Losing the node-owned bridge must close the listener and all live TCP
	// connections without affecting the workload's execution lifecycle.
	if err := hostOutput.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("bridge shutdown: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("agent did not close after bridge EOF")
	}
	if _, err := conn.Read(response[:]); err != io.EOF {
		t.Fatalf("live connection after bridge EOF: %v, want EOF", err)
	}
}

func TestAgentCancellationClosesLiveConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	guestInput, hostOutput := io.Pipe()
	hostInput, guestOutput := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- serveListener(ctx, listener, guestInput, guestOutput) }()
	var ready [len(stdioframe.ReadyLine)]byte
	if _, err := io.ReadFull(hostInput, ready[:]); err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := stdioframe.New(hostInput, hostOutput).Recv(); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("canceled agent: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("canceled agent kept a live listener or connection")
	}
	var buf [1]byte
	if _, err := conn.Read(buf[:]); err != io.EOF {
		t.Fatalf("connection after cancellation = %v, want EOF", err)
	}
}

func TestSlowConsumerUnblocksOnConnectionClose(t *testing.T) {
	conn := &blockedConn{entered: make(chan struct{}), closed: make(chan struct{})}
	stream := &queuedStream{frames: make(chan *tunnelv1.TunnelFrame, 1)}
	peer := &agentPeer{stream: stream, conns: map[uint64]net.Conn{7: conn}}
	finished := make(chan error, 1)
	go func() { finished <- peer.recvLoop() }()
	stream.frames <- &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamData{
		StreamData: &tunnelv1.StreamData{StreamID: 7, Data: []byte("blocked")},
	}}
	<-conn.entered // The write is blocked by an unread consumer, not a timer.
	peer.closeAll()
	close(stream.frames)
	select {
	case err := <-finished:
		if err != io.EOF {
			t.Fatalf("recvLoop after slow consumer close = %v, want EOF", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("slow consumer blocked tunnel shutdown")
	}
}

func TestAgentDeliversEntireFrameDespiteShortTCPWrites(t *testing.T) {
	conn := &scriptedWriteConn{maxWrite: 2}
	if err := writeConn(conn, []byte("complete")); err != nil {
		t.Fatal(err)
	}
	if got := conn.written.String(); got != "complete" {
		t.Fatalf("TCP bytes = %q, want complete", got)
	}
	if conn.writeDeadline.IsZero() {
		t.Fatal("TCP write had no bounded deadline")
	}
	if conn.closed {
		t.Fatal("healthy stream was closed after a short write")
	}
}

func TestAgentClosesStreamAfterUnprogressingTCPWrite(t *testing.T) {
	conn := &scriptedWriteConn{maxWrite: 0}
	stream := &queuedStream{frames: make(chan *tunnelv1.TunnelFrame, 1)}
	peer := &agentPeer{stream: stream, conns: map[uint64]net.Conn{7: conn}}
	stream.frames <- &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamData{
		StreamData: &tunnelv1.StreamData{StreamID: 7, Data: []byte("undeliverable")},
	}}
	close(stream.frames)
	if err := peer.recvLoop(); err != io.EOF {
		t.Fatalf("recvLoop = %v, want EOF", err)
	}
	if !conn.closed || len(peer.conns) != 0 {
		t.Fatalf("failed TCP stream remained active: closed=%t streams=%d", conn.closed, len(peer.conns))
	}
}

func TestDataBeforeCloseIsFullyDelivered(t *testing.T) {
	conn := &scriptedWriteConn{maxWrite: 64 << 10}
	data := bytes.Repeat([]byte("z"), stdioframe.MaxDataBytes)
	stream := &queuedStream{frames: make(chan *tunnelv1.TunnelFrame, 2)}
	peer := &agentPeer{stream: stream, conns: map[uint64]net.Conn{7: conn}}
	stream.frames <- &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamData{
		StreamData: &tunnelv1.StreamData{StreamID: 7, Data: data},
	}}
	stream.frames <- &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamClose{
		StreamClose: &tunnelv1.StreamClose{StreamID: 7},
	}}
	close(stream.frames)
	if err := peer.recvLoop(); err != io.EOF {
		t.Fatalf("recvLoop = %v, want EOF", err)
	}
	if !bytes.Equal(conn.written.Bytes(), data) || !conn.closed {
		t.Fatalf("Data then Close produced %d of %d bytes; closed=%t", conn.written.Len(), len(data), conn.closed)
	}
}

func TestTimedOutWriteAllowsOtherStreamAndPing(t *testing.T) {
	stalledConn := &timedOutConn{entered: make(chan struct{}), expire: make(chan struct{}), closed: make(chan struct{})}
	healthyConn := &scriptedWriteConn{maxWrite: 32, wrote: make(chan struct{})}
	stream := &queuedStream{
		frames: make(chan *tunnelv1.TunnelFrame, 3),
		sent:   make(chan *tunnelv1.TunnelFrame, 1),
	}
	peer := &agentPeer{stream: stream, conns: map[uint64]net.Conn{1: stalledConn, 2: healthyConn}}
	finished := make(chan error, 1)
	go func() { finished <- peer.recvLoop() }()
	sendData := func(id uint64, data string) {
		stream.frames <- &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_StreamData{
			StreamData: &tunnelv1.StreamData{StreamID: id, Data: []byte(data)},
		}}
	}
	sendData(1, "blocked")
	<-stalledConn.entered // The first frame is stuck in the TCP write.
	if stalledConn.writeDeadline.IsZero() {
		t.Fatal("stalled TCP write had no deadline")
	}
	sendData(2, "healthy")
	stream.frames <- &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_Ping{Ping: &tunnelv1.Ping{ID: "still-live"}}}
	close(stream.frames)
	close(stalledConn.expire) // Deterministically model expiration; no sleeps.
	select {
	case frame := <-stream.sent:
		if frame.GetPong().GetID() != "still-live" {
			t.Fatalf("relay response = %v, want Pong", frame)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stalled stream blocked relay Pong")
	}
	select {
	case <-healthyConn.wrote:
	case <-time.After(5 * time.Second):
		t.Fatal("stalled stream blocked a second stream")
	}
	if got := healthyConn.written.String(); got != "healthy" {
		t.Fatalf("second stream bytes = %q", got)
	}
	select {
	case <-stalledConn.closed:
	case <-time.After(5 * time.Second):
		t.Fatal("write timeout did not close the stalled stream")
	}
	peer.closeAll()
	if err := <-finished; err != io.EOF {
		t.Fatalf("recvLoop = %v, want EOF", err)
	}
}

type scriptedWriteConn struct {
	written       bytes.Buffer
	maxWrite      int
	closed        bool
	wrote         chan struct{}
	once          sync.Once
	writeDeadline time.Time
}

func (*scriptedWriteConn) Read([]byte) (int, error) { return 0, io.EOF }

func (c *scriptedWriteConn) Write(data []byte) (int, error) {
	if c.maxWrite == 0 {
		return 0, nil
	}
	if len(data) > c.maxWrite {
		data = data[:c.maxWrite]
	}
	n, err := c.written.Write(data)
	if c.wrote != nil {
		c.once.Do(func() { close(c.wrote) })
	}
	return n, err
}

func (c *scriptedWriteConn) Close() error {
	c.closed = true
	return nil
}

func (*scriptedWriteConn) LocalAddr() net.Addr             { return &net.TCPAddr{} }
func (*scriptedWriteConn) RemoteAddr() net.Addr            { return &net.TCPAddr{} }
func (*scriptedWriteConn) SetDeadline(time.Time) error     { return nil }
func (*scriptedWriteConn) SetReadDeadline(time.Time) error { return nil }
func (c *scriptedWriteConn) SetWriteDeadline(deadline time.Time) error {
	c.writeDeadline = deadline
	return nil
}

type queuedStream struct {
	frames chan *tunnelv1.TunnelFrame
	sent   chan *tunnelv1.TunnelFrame
}

func (s *queuedStream) Send(frame *tunnelv1.TunnelFrame) error {
	if s.sent != nil {
		s.sent <- frame
	}
	return nil
}

func (s *queuedStream) Recv() (*tunnelv1.TunnelFrame, error) {
	frame, ok := <-s.frames
	if !ok {
		return nil, io.EOF
	}
	return frame, nil
}

type blockedConn struct {
	entered chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func (c *blockedConn) Read([]byte) (int, error) {
	<-c.closed
	return 0, io.EOF
}

func (c *blockedConn) Write([]byte) (int, error) {
	select {
	case <-c.entered:
	default:
		close(c.entered)
	}
	<-c.closed
	return 0, net.ErrClosed
}

func (c *blockedConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (*blockedConn) LocalAddr() net.Addr              { return &net.TCPAddr{} }
func (*blockedConn) RemoteAddr() net.Addr             { return &net.TCPAddr{} }
func (*blockedConn) SetDeadline(time.Time) error      { return nil }
func (*blockedConn) SetReadDeadline(time.Time) error  { return nil }
func (*blockedConn) SetWriteDeadline(time.Time) error { return nil }

type timedOutConn struct {
	entered       chan struct{}
	expire        chan struct{}
	closed        chan struct{}
	once          sync.Once
	writeDeadline time.Time
}

func (c *timedOutConn) Read([]byte) (int, error) {
	<-c.closed
	return 0, io.EOF
}

func (c *timedOutConn) Write([]byte) (int, error) {
	close(c.entered)
	select {
	case <-c.expire:
		return 0, os.ErrDeadlineExceeded
	case <-c.closed:
		return 0, net.ErrClosed
	}
}

func (c *timedOutConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return nil
}

func (*timedOutConn) LocalAddr() net.Addr             { return &net.TCPAddr{} }
func (*timedOutConn) RemoteAddr() net.Addr            { return &net.TCPAddr{} }
func (*timedOutConn) SetDeadline(time.Time) error     { return nil }
func (*timedOutConn) SetReadDeadline(time.Time) error { return nil }
func (c *timedOutConn) SetWriteDeadline(deadline time.Time) error {
	c.writeDeadline = deadline
	return nil
}
