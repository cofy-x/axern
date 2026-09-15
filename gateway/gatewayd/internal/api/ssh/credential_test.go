package sshapi

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	term "github.com/cofy-x/axern/gateway/gatewayd/internal/application/terminal"
	"golang.org/x/crypto/ssh"
)

type credentialManager struct {
	allocation string
	err        error
}

func (m *credentialManager) Authorize(_ context.Context, id string) error {
	m.allocation = id
	return m.err
}
func (*credentialManager) Options() term.Options { return term.Options{} }
func (*credentialManager) OpenWithOptions(context.Context, string, term.OpenOptions) (*term.Session, error) {
	return nil, errors.New("unexpected process creation during authentication")
}

type credentialMetadata struct{ user string }

func (m credentialMetadata) User() string        { return m.user }
func (credentialMetadata) SessionID() []byte     { return nil }
func (credentialMetadata) ClientVersion() []byte { return nil }
func (credentialMetadata) ServerVersion() []byte { return nil }
func (credentialMetadata) RemoteAddr() net.Addr  { return &net.TCPAddr{} }
func (credentialMetadata) LocalAddr() net.Addr   { return &net.TCPAddr{} }

func TestSSHRequiresAllocationCredentialAuthorization(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "test")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "host_key")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	manager := &credentialManager{}
	server, err := New("127.0.0.1:0", path, manager, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	permissions, err := server.config.PublicKeyCallback(credentialMetadata{user: "allocation-a"}, signer.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	if manager.allocation != "allocation-a" || permissions.Extensions["credential_fingerprint"] != fmt.Sprintf("%x", sha256.Sum256(signer.PublicKey().Marshal())) {
		t.Fatal("SSH did not bind the exact key and Allocation")
	}
	manager.err = errors.New("namespace denied")
	if _, err := server.config.PublicKeyCallback(credentialMetadata{user: "allocation-b"}, signer.PublicKey()); err == nil {
		t.Fatal("denied Allocation accepted")
	}
	if _, err := server.config.PublicKeyCallback(credentialMetadata{}, signer.PublicKey()); err == nil {
		t.Fatal("empty Allocation accepted")
	}
	if _, err := server.config.PublicKeyCallback(credentialMetadata{user: "allocation-a"}, &ssh.Certificate{Key: signer.PublicKey()}); err == nil {
		t.Fatal("SSH certificate accepted as plain key")
	}
}
