package tunnel

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	tunnelcontrolv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/tunnel/v1"
	tunnelv1 "github.com/cofy-x/axern/sdk/go/gen/axern/tunnel/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	grpcstatus "google.golang.org/grpc/status"
)

type echoRelay struct {
	tunnelv1.UnimplementedTunnelRelayServer
}

func (echoRelay) ConnectPeer(stream tunnelv1.TunnelRelay_ConnectPeerServer) error {
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		if err := stream.Send(frame); err != nil {
			return err
		}
	}
}

func TestConnectPeerProxiesClientPeer(t *testing.T) {
	certs := writeRelayTLSFiles(t)
	creds, err := credentials.NewServerTLSFromFile(certs.certPath, certs.keyPath)
	if err != nil {
		t.Fatalf("NewServerTLSFromFile() error = %v", err)
	}
	backendServer := grpc.NewServer(grpc.Creds(creds))
	tunnelv1.RegisterTunnelRelayServer(backendServer, echoRelay{})
	backendAddr := serveGRPC(t, backendServer)

	proxy, err := New(Options{Target: backendAddr, CACert: certs.caPath})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	proxyServer := grpc.NewServer()
	tunnelv1.RegisterTunnelRelayServer(proxyServer, proxy)
	proxyAddr := serveGRPC(t, proxyServer)

	conn, err := grpc.NewClient(proxyAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	stream, err := tunnelv1.NewTunnelRelayClient(conn).ConnectPeer(context.Background())
	if err != nil {
		t.Fatalf("ConnectPeer() error = %v", err)
	}
	want := &tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_PeerOpen{PeerOpen: &tunnelv1.PeerOpen{
		SessionID: "session-1",
		PeerKind:  tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_CLIENT,
		Token:     "secret",
	}}}
	if err := stream.Send(want); err != nil {
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

func TestConnectPeerRejectsNodePeer(t *testing.T) {
	proxy, err := New(Options{Target: "127.0.0.1:1"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	proxyServer := grpc.NewServer()
	tunnelv1.RegisterTunnelRelayServer(proxyServer, proxy)
	proxyAddr := serveGRPC(t, proxyServer)

	conn, err := grpc.NewClient(proxyAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	stream, err := tunnelv1.NewTunnelRelayClient(conn).ConnectPeer(context.Background())
	if err != nil {
		t.Fatalf("ConnectPeer() error = %v", err)
	}
	if err := stream.Send(&tunnelv1.TunnelFrame{Payload: &tunnelv1.TunnelFrame_PeerOpen{PeerOpen: &tunnelv1.PeerOpen{
		SessionID: "session-1",
		PeerKind:  tunnelcontrolv1.TunnelPeerKind_TUNNEL_PEER_KIND_NODE,
		Token:     "secret",
	}}}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	_, err = stream.Recv()
	if grpcstatus.Code(err) != codes.PermissionDenied {
		t.Fatalf("Recv() error code = %s, want PermissionDenied; err=%v", grpcstatus.Code(err), err)
	}
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

type relayTLSFiles struct {
	caPath   string
	certPath string
	keyPath  string
}

func writeRelayTLSFiles(t *testing.T) relayTLSFiles {
	t.Helper()
	dir := t.TempDir()
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate ca key: %v", err)
	}
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "axern-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create ca cert: %v", err)
	}
	serverKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "localhost"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	serverDER, err := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create server cert: %v", err)
	}
	caPath := filepath.Join(dir, "ca.crt")
	certPath := filepath.Join(dir, "relay.crt")
	keyPath := filepath.Join(dir, "relay.key")
	writePEM(t, caPath, "CERTIFICATE", caDER)
	writePEM(t, certPath, "CERTIFICATE", serverDER)
	writePEM(t, keyPath, "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(serverKey))
	return relayTLSFiles{caPath: caPath, certPath: certPath, keyPath: keyPath}
}

func writePEM(t *testing.T, path, blockType string, bytes []byte) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer file.Close()
	if err := pem.Encode(file, &pem.Block{Type: blockType, Bytes: bytes}); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
