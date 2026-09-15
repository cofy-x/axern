// Package workloadtls defines the identity carried by Axern internal TLS peers.
// Certificate verification and Node admission remain separate responsibilities.
package workloadtls

import (
	"crypto/x509"
	"fmt"
	"net/url"
	"regexp"
)

var component = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,252}$`)
var trustDomain = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{0,252}$`)

type Identity struct {
	Cluster string
	Role    string
	NodeID  string
}

// ValidateNodeID applies the same identifier contract at admission and TLS boundaries.
func ValidateNodeID(nodeID string) error {
	if !component.MatchString(nodeID) {
		return fmt.Errorf("invalid workload Node identity")
	}
	return nil
}

func (i Identity) URI() (*url.URL, error) {
	if !trustDomain.MatchString(i.Cluster) {
		return nil, fmt.Errorf("invalid workload trust domain")
	}
	path := "/service/" + i.Role
	switch i.Role {
	case "axnoded":
		if err := ValidateNodeID(i.NodeID); err != nil {
			return nil, err
		}
		path = "/node/" + i.NodeID
	case "controld", "gatewayd", "tunneld":
		if i.NodeID != "" {
			return nil, fmt.Errorf("service identity cannot contain a Node ID")
		}
	default:
		return nil, fmt.Errorf("unsupported workload role")
	}
	return &url.URL{Scheme: "spiffe", Host: i.Cluster, Path: path}, nil
}

// FromCertificate never falls back to CN or DNS SAN and rejects ambiguity.
// The caller must obtain cert from a successfully verified TLS chain.
func FromCertificate(cert *x509.Certificate, cluster string) (Identity, error) {
	if cert == nil || len(cert.URIs) != 1 || cert.URIs[0] == nil {
		return Identity{}, fmt.Errorf("exactly one workload URI SAN is required")
	}
	u := cert.URIs[0]
	i := Identity{Cluster: cluster}
	if len(u.Path) > 6 && u.Path[:6] == "/node/" {
		i.Role = "axnoded"
		i.NodeID = u.Path[6:]
	} else if len(u.Path) > 9 && u.Path[:9] == "/service/" {
		i.Role = u.Path[9:]
	}
	want, err := i.URI()
	if err != nil || u.String() != want.String() {
		return Identity{}, fmt.Errorf("invalid workload URI SAN or trust domain")
	}
	return i, nil
}
