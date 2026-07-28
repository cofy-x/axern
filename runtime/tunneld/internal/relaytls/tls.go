package relaytls

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type ClientConfig struct {
	CACert     string
	CAPEM      string
	ServerName string
}

type ServerConfig struct {
	Cert string
	Key  string
}

func DialOptions(cfg ClientConfig) ([]grpc.DialOption, error) {
	caPEM := []byte(cfg.CAPEM)
	if len(caPEM) == 0 {
		if cfg.CACert == "" {
			return nil, fmt.Errorf("relay TLS requires a CA certificate")
		}
		var err error
		caPEM, err = os.ReadFile(cfg.CACert)
		if err != nil {
			return nil, err
		}
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("parse relay tls ca cert %q", cfg.CACert)
	}
	return []grpc.DialOption{grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
		ServerName: cfg.ServerName,
	}))}, nil
}

func ServerOptions(cfg ServerConfig) ([]grpc.ServerOption, error) {
	if cfg.Cert == "" || cfg.Key == "" {
		return nil, fmt.Errorf("relay TLS requires server cert/key")
	}
	cert, err := tls.LoadX509KeyPair(cfg.Cert, cfg.Key)
	if err != nil {
		return nil, err
	}
	return []grpc.ServerOption{grpc.Creds(credentials.NewTLS(&tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}))}, nil
}
