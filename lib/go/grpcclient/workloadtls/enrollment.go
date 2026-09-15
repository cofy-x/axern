package workloadtls

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"os"

	"google.golang.org/grpc/credentials"
)

// BootstrapCredentials authenticates exactly the control-plane workload before
// sending an enrollment token. It never supplies a client certificate and must
// only be used for the enrollment service, not ordinary control-plane traffic.
type BootstrapCredentials struct {
	TrustPath string
	Peer      Identity
}

func (c *BootstrapCredentials) ClientHandshake(ctx context.Context, authority string, conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if c.Peer.Role != "controld" {
		return nil, nil, fmt.Errorf("enrollment requires controld identity")
	}
	trust, err := os.ReadFile(c.TrustPath)
	if err != nil {
		return nil, nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(trust) {
		return nil, nil, fmt.Errorf("invalid enrollment trust bundle")
	}
	config := &tls.Config{MinVersion: tls.VersionTLS13, SessionTicketsDisabled: true,
		// Complete chain, time, usage and exact URI verification below replaces
		// DNS verification; this is not an insecure enrollment mode.
		InsecureSkipVerify: true,
		VerifyConnection: func(state tls.ConnectionState) error {
			if len(state.PeerCertificates) == 0 {
				return fmt.Errorf("enrollment server certificate required")
			}
			intermediates := x509.NewCertPool()
			for _, cert := range state.PeerCertificates[1:] {
				intermediates.AddCert(cert)
			}
			chains, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
			if err != nil {
				return err
			}
			id, err := FromCertificate(chains[0][0], c.Peer.Cluster)
			if err != nil || id != c.Peer {
				return fmt.Errorf("enrollment server identity mismatch")
			}
			return nil
		},
	}
	return credentials.NewTLS(config).ClientHandshake(ctx, authority, conn)
}
func (c *BootstrapCredentials) ServerHandshake(net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return nil, nil, fmt.Errorf("bootstrap credentials are client-only")
}
func (c *BootstrapCredentials) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "tls", SecurityVersion: "1.3"}
}
func (c *BootstrapCredentials) Clone() credentials.TransportCredentials {
	return &BootstrapCredentials{TrustPath: c.TrustPath, Peer: c.Peer}
}
func (c *BootstrapCredentials) OverrideServerName(string) error { return nil }

// EnrollmentCredentials is server-only and belongs to a listener exposing only
// NodeEnrollment. Enroll authenticates a finite token; Renew requires a verified
// certificate in its handler. All other workload listeners require mTLS.
type EnrollmentCredentials struct{ Workload *Credentials }

func (c *EnrollmentCredentials) ServerHandshake(conn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	if c.Workload == nil || c.Workload.Local.Role != "controld" {
		return nil, nil, fmt.Errorf("enrollment requires controld credentials")
	}
	config, err := c.Workload.config(true)
	if err != nil {
		return nil, nil, err
	}
	config.ClientAuth = tls.VerifyClientCertIfGiven
	verify := config.VerifyConnection
	config.VerifyConnection = func(state tls.ConnectionState) error {
		if len(state.PeerCertificates) == 0 {
			return nil
		}
		return verify(state)
	}
	return credentials.NewTLS(config).ServerHandshake(conn)
}
func (c *EnrollmentCredentials) ClientHandshake(context.Context, string, net.Conn) (net.Conn, credentials.AuthInfo, error) {
	return nil, nil, fmt.Errorf("enrollment listener credentials are server-only")
}
func (c *EnrollmentCredentials) Info() credentials.ProtocolInfo {
	return credentials.ProtocolInfo{SecurityProtocol: "tls", SecurityVersion: "1.3"}
}
func (c *EnrollmentCredentials) Clone() credentials.TransportCredentials {
	if c.Workload == nil {
		return &EnrollmentCredentials{}
	}
	return &EnrollmentCredentials{Workload: c.Workload.Clone().(*Credentials)}
}
func (c *EnrollmentCredentials) OverrideServerName(string) error { return nil }
