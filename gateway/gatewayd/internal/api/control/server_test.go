package control

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	grpcstatus "google.golang.org/grpc/status"
)

type testControlService interface{}

type testTunnelRelay struct {
	tunnelv1.UnimplementedTunnelRelayServer
}

func (testTunnelRelay) ConnectPeer(stream tunnelv1.TunnelRelay_ConnectPeerServer) error {
	frame, err := stream.Recv()
	if err != nil {
		return err
	}
	return stream.Send(frame)
}

func TestProxyUnknownServiceForwardsUnaryControlRPC(t *testing.T) {
	backendServer := grpc.NewServer(grpc.ForceServerCodec(rawCodec{}))
	backendServer.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Control",
		HandlerType: (*testControlService)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Echo",
			Handler:    echoUnaryHandler,
		}},
	}, struct{}{})
	backendAddr := serveGRPC(t, backendServer)
	backendConn, err := grpc.NewClient(
		backendAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rawCodec{})),
	)
	if err != nil {
		t.Fatalf("dial backend: %v", err)
	}
	defer backendConn.Close()

	proxyServer := grpc.NewServer(
		grpc.ForceServerCodec(rawCodec{}),
		grpc.UnknownServiceHandler(proxyUnknownServiceForServices(backendConn, map[string]struct{}{"test.Control": {}})),
	)
	proxyAddr := serveGRPC(t, proxyServer)
	proxyConn, err := grpc.NewClient(
		proxyAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rawCodec{})),
	)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer proxyConn.Close()

	ctx := metadata.NewOutgoingContext(context.Background(), metadata.Pairs("x-proxy-test", "present"))
	var out rawMessage
	var header metadata.MD
	var trailer metadata.MD
	if err := proxyConn.Invoke(ctx, "/test.Control/Echo", rawMessage("hello"), &out, grpc.Header(&header), grpc.Trailer(&trailer)); err != nil {
		t.Fatalf("Invoke() error = %v", err)
	}
	if string(out) != "echo:hello:present" {
		t.Fatalf("response = %q, want echo:hello:present", string(out))
	}
	if got := header.Get("x-proxy-header"); len(got) != 1 || got[0] != "ok" {
		t.Fatalf("header x-proxy-header = %v, want [ok]", got)
	}
	if got := trailer.Get("x-proxy-trailer"); len(got) != 1 || got[0] != "done" {
		t.Fatalf("trailer x-proxy-trailer = %v, want [done]", got)
	}
}

func TestProxyUnknownServiceForwardsServerStreamingControlRPC(t *testing.T) {
	backendServer := grpc.NewServer(grpc.ForceServerCodec(rawCodec{}))
	backendServer.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Control",
		HandlerType: (*testControlService)(nil),
		Streams: []grpc.StreamDesc{{
			StreamName:    "Watch",
			Handler:       echoServerStreamHandler,
			ServerStreams: true,
		}},
	}, struct{}{})
	backendAddr := serveGRPC(t, backendServer)
	backendConn, err := grpc.NewClient(
		backendAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rawCodec{})),
	)
	if err != nil {
		t.Fatalf("dial backend: %v", err)
	}
	defer backendConn.Close()

	proxyServer := grpc.NewServer(
		grpc.ForceServerCodec(rawCodec{}),
		grpc.UnknownServiceHandler(proxyUnknownServiceForServices(backendConn, map[string]struct{}{"test.Control": {}})),
	)
	proxyAddr := serveGRPC(t, proxyServer)
	proxyConn, err := grpc.NewClient(
		proxyAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rawCodec{})),
	)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer proxyConn.Close()

	var header metadata.MD
	var trailer metadata.MD
	stream, err := proxyConn.NewStream(
		context.Background(),
		&grpc.StreamDesc{ServerStreams: true},
		"/test.Control/Watch",
		grpc.Header(&header),
		grpc.Trailer(&trailer),
	)
	if err != nil {
		t.Fatalf("NewStream() error = %v", err)
	}
	if err := stream.SendMsg(rawMessage("hello")); err != nil {
		t.Fatalf("SendMsg() error = %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("CloseSend() error = %v", err)
	}
	for index, want := range []string{"watch:hello:1", "watch:hello:2"} {
		var response rawMessage
		if err := stream.RecvMsg(&response); err != nil {
			t.Fatalf("RecvMsg(%d) error = %v", index, err)
		}
		if string(response) != want {
			t.Fatalf("response %d = %q, want %q", index, response, want)
		}
	}
	var response rawMessage
	if err := stream.RecvMsg(&response); !errors.Is(err, io.EOF) {
		t.Fatalf("RecvMsg() error = %v, want EOF", err)
	}
	if got := header.Get("x-proxy-stream-header"); len(got) != 1 || got[0] != "ok" {
		t.Fatalf("header x-proxy-stream-header = %v, want [ok]", got)
	}
	if got := trailer.Get("x-proxy-stream-trailer"); len(got) != 1 || got[0] != "done" {
		t.Fatalf("trailer x-proxy-stream-trailer = %v, want [done]", got)
	}
}

func TestProxyUnknownServiceRejectsNonPublicControlService(t *testing.T) {
	backendServer := grpc.NewServer(grpc.ForceServerCodec(rawCodec{}))
	backendServer.RegisterService(&grpc.ServiceDesc{
		ServiceName: "test.Control",
		HandlerType: (*testControlService)(nil),
		Methods: []grpc.MethodDesc{{
			MethodName: "Echo",
			Handler:    echoUnaryHandler,
		}},
	}, struct{}{})
	backendAddr := serveGRPC(t, backendServer)
	backendConn, err := grpc.NewClient(
		backendAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rawCodec{})),
	)
	if err != nil {
		t.Fatalf("dial backend: %v", err)
	}
	defer backendConn.Close()

	proxyServer := grpc.NewServer(
		grpc.ForceServerCodec(rawCodec{}),
		grpc.UnknownServiceHandler(proxyUnknownServiceForServices(backendConn, publicControlServices)),
	)
	proxyAddr := serveGRPC(t, proxyServer)
	proxyConn, err := grpc.NewClient(
		proxyAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rawCodec{})),
	)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer proxyConn.Close()

	var out rawMessage
	err = proxyConn.Invoke(context.Background(), "/axern.control.node.v1.NodeControl/RegisterNode", rawMessage("hello"), &out)
	if grpcstatus.Code(err) != codes.PermissionDenied {
		t.Fatalf("Invoke() error code = %s, want PermissionDenied; err=%v", grpcstatus.Code(err), err)
	}
}

func TestRawCodecPreservesRegisteredProtoServices(t *testing.T) {
	server := grpc.NewServer(grpc.ForceServerCodec(rawCodec{}))
	tunnelv1.RegisterTunnelRelayServer(server, testTunnelRelay{})
	addr := serveGRPC(t, server)
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial server: %v", err)
	}
	defer conn.Close()
	stream, err := tunnelv1.NewTunnelRelayClient(conn).ConnectPeer(context.Background())
	if err != nil {
		t.Fatalf("ConnectPeer() error = %v", err)
	}
	if err := stream.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_PeerOpen{PeerOpen: &tunnelv1.PeerOpen{
		SessionID: "session-1",
		PeerKind:  tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT,
		Token:     "secret",
	}}}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	got, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv() error = %v", err)
	}
	if got.GetPeerOpen().GetSessionID() != "session-1" {
		t.Fatalf("session = %q, want session-1", got.GetPeerOpen().GetSessionID())
	}
}

func echoUnaryHandler(_ any, ctx context.Context, dec func(any) error, _ grpc.UnaryServerInterceptor) (any, error) {
	var in rawMessage
	if err := dec(&in); err != nil {
		return nil, err
	}
	md, _ := metadata.FromIncomingContext(ctx)
	if err := grpc.SendHeader(ctx, metadata.Pairs("x-proxy-header", "ok")); err != nil {
		return nil, err
	}
	grpc.SetTrailer(ctx, metadata.Pairs("x-proxy-trailer", "done"))
	return rawMessage("echo:" + string(in) + ":" + first(md.Get("x-proxy-test"))), nil
}

func echoServerStreamHandler(_ any, stream grpc.ServerStream) error {
	var in rawMessage
	if err := stream.RecvMsg(&in); err != nil {
		return err
	}
	if err := stream.SendHeader(metadata.Pairs("x-proxy-stream-header", "ok")); err != nil {
		return err
	}
	stream.SetTrailer(metadata.Pairs("x-proxy-stream-trailer", "done"))
	for _, suffix := range []string{"1", "2"} {
		if err := stream.SendMsg(rawMessage("watch:" + string(in) + ":" + suffix)); err != nil {
			return err
		}
	}
	return nil
}

func first(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func serveGRPC(t *testing.T, server *grpc.Server) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(server.Stop)
	go func() {
		_ = server.Serve(listener)
	}()
	return listener.Addr().String()
}
