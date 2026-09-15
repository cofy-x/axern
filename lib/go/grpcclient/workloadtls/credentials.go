package workloadtls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	"google.golang.org/grpc/credentials"
)

// Credentials loads one atomic PEM bundle and the trust bundle for each new
// handshake. A failed reload fails the handshake, never uses stale authority.
// Existing transports must be bounded/re-authorized by their owning protocol.
type Credentials struct {
	BundlePath string
	TrustPath  string
	Local      Identity
	Peer       Identity
	mu         sync.RWMutex
	serverName string
}

func (c *Credentials) config(server bool) (*tls.Config, error) {
	data, err := os.ReadFile(c.BundlePath)
	if err != nil {
		return nil, fmt.Errorf("read workload credential: %w", err)
	}
	pair, err := tls.X509KeyPair(data, data)
	if err != nil {
		return nil, fmt.Errorf("parse workload credential: %w", err)
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		return nil, err
	}
	identity, err := FromCertificate(leaf, c.Local.Cluster)
	if err != nil || identity != c.Local {
		return nil, fmt.Errorf("local workload identity mismatch")
	}
	trust, err := os.ReadFile(c.TrustPath)
	if err != nil {
		return nil, fmt.Errorf("read workload trust: %w", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(trust) {
		return nil, fmt.Errorf("invalid workload trust bundle")
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, SessionTicketsDisabled: true}
	if server {
		config.ClientCAs = roots
		config.ClientAuth = tls.RequireAndVerifyClientCert
		config.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.VerifiedChains) == 0 {
				return fmt.Errorf("verified workload peer required")
			}
			_, err := FromCertificate(state.VerifiedChains[0][0], c.Local.Cluster)
			return err
		}
	} else {
		// Workload servers are addressed by a verified URI identity, not a DNS
		// identity. VerifyConnection performs complete chain/time/usage checking
		// against the configured roots before comparing that exact URI identity.
		config.InsecureSkipVerify = true
		config.VerifyConnection = func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return fmt.Errorf("workload server certificate required")
			}
			intermediates := x509.NewCertPool()
			for _, cert := range state.PeerCertificates[1:] {
				intermediates.AddCert(cert)
			}
			chains, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
			if err != nil {
				return err
			}
			identity, err := FromCertificate(chains[0][0], c.Peer.Cluster)
			if err != nil || identity != c.Peer {
				return fmt.Errorf("workload server identity mismatch")
			}
			return nil
		}
	}
	c.mu.RLock()
	config.ServerName = c.serverName
	c.mu.RUnlock()
	return config, nil
}

func (c *Credentials) ClientHandshake(ctx context.Context, authority string, conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	config, err := c.config(false)
	if err != nil {
		return nil, nil, err
	}
	secured, auth, err := credentials.NewTLS(config).ClientHandshake(ctx, authority, conn)
	return boundCertificateLifetime(secured, auth, err, config.Certificates[0])
}
func (c *Credentials) ServerHandshake(conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	config, err := c.config(true)
	if err != nil {
		return nil, nil, err
	}
	secured, auth, err := credentials.NewTLS(config).ServerHandshake(conn)
	return boundCertificateLifetime(secured, auth, err, config.Certificates[0])
}
func (c *Credentials) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "tls", SecurityVersion: "1.3"}
}
func (c *Credentials) Clone() credentials.TransportCredentials {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return &Credentials{BundlePath: c.BundlePath, TrustPath: c.TrustPath, Local: c.Local, Peer: c.Peer, serverName: c.serverName}
}
func (c *Credentials) OverrideServerName(name string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.serverName = name
	return nil
}

// Bound the transport itself, not only new RPCs. A cached HTTP/2 connection must
// not keep a replaced or expired leaf usable indefinitely. The I/O deadline
// is not sufficient: gRPC clears raw connection deadlines after HTTP/2 setup.
// A connection-owned timer closes idle reads too; reconnect loads the new bundle.
func boundCertificateLifetime(conn net.Conn, auth credentials.AuthInfo, handshakeErr error, local tls.Certificate) (net.Conn, credentials.AuthInfo, error) {
	if handshakeErr != nil {
		return conn, auth, handshakeErr
	}
	info, ok := auth.(credentials.TLSInfo)
	if !ok {
		conn.Close()
		return nil, nil, fmt.Errorf("TLS authentication information is required")
	}
	var deadline time.Time
	include := func(cert *x509.Certificate) {
		if deadline.IsZero() || cert.NotAfter.Before(deadline) {
			deadline = cert.NotAfter
		}
	}
	for _, der := range local.Certificate {
		cert, err := x509.ParseCertificate(der)
		if err != nil {
			conn.Close()
			return nil, nil, err
		}
		include(cert)
	}
	if len(info.State.VerifiedChains) > 0 {
		for _, cert := range info.State.VerifiedChains[0] {
			include(cert)
		}
	} else {
		// URI clients verify their chain in VerifyConnection rather than using Go's
		// DNS verifier, so VerifiedChains is not populated in ConnectionState.
		for _, cert := range info.State.PeerCertificates {
			include(cert)
		}
	}
	if deadline.IsZero() || !time.Now().Before(deadline) {
		conn.Close()
		return nil, nil, fmt.Errorf("workload certificate has expired")
	}
	bounded := &certificateConn{Conn: conn}
	bounded.expiry = time.AfterFunc(time.Until(deadline), func() {
		// Unblock application I/O immediately, even while TLS sends close_notify.
		_ = conn.SetDeadline(time.Now())
		_ = conn.Close()
	})
	return bounded, auth, nil
}

// Keep certificate expiry independent of protocol-owned read/write deadlines.
// Closing early releases the timer; no goroutine or historical state survives it.
type certificateConn struct {
	net.Conn
	expiry *time.Timer
}

func (c *certificateConn) Close() error {
	c.expiry.Stop()
	return c.Conn.Close()
}
