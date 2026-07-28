package sshapi

import (
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	gossh "golang.org/x/crypto/ssh"
)

func TestParseAuthorizedKeysAcceptsKnownKey(t *testing.T) {
	t.Parallel()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	line := gossh.MarshalAuthorizedKey(signer.PublicKey())
	keys, err := ParseAuthorizedKeys(append([]byte("# comment\n\n"), line...))
	if err != nil {
		t.Fatalf("ParseAuthorizedKeys() error = %v", err)
	}
	if !keys.Contains(signer.PublicKey()) {
		t.Fatal("authorized keys did not contain parsed key")
	}
}

func TestParseAuthorizedKeysRejectsMalformed(t *testing.T) {
	t.Parallel()
	if _, err := ParseAuthorizedKeys([]byte("not-a-key\n")); err == nil {
		t.Fatal("ParseAuthorizedKeys() error = nil, want malformed key error")
	}
}

func TestAuthorizedKeysRejectsUnknownKey(t *testing.T) {
	t.Parallel()
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	known, err := gossh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := ParseAuthorizedKeys(gossh.MarshalAuthorizedKey(known.PublicKey()))
	if err != nil {
		t.Fatal(err)
	}

	_, otherPrivateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := gossh.NewSignerFromKey(otherPrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	if keys.Contains(other.PublicKey()) {
		t.Fatal("authorized keys accepted unknown key")
	}
}
