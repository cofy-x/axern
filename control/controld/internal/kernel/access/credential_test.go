package access

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestSSHCredentialRequiresOnePlainKeyAndExpiry(t *testing.T) {
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	key, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	encoded := string(ssh.MarshalAuthorizedKey(key))
	expires := time.Now().Add(time.Hour)
	material, err := ParseCredentialMaterial(nil, encoded, expires)
	if err != nil {
		t.Fatal(err)
	}
	if material.Kind != CredentialSSH || material.Fingerprint != sha256.Sum256(key.Marshal()) || !material.ExpiresAt.Equal(expires) {
		t.Fatalf("incorrect SSH credential: %+v", material)
	}
	for _, input := range []struct {
		der    []byte
		key    string
		expiry time.Time
	}{
		{nil, "", expires}, {[]byte("certificate"), encoded, expires},
		{nil, encoded, time.Time{}}, {nil, encoded + encoded, expires},
		{nil, "command=\"echo forbidden\" " + encoded, expires},
	} {
		if _, err := ParseCredentialMaterial(input.der, input.key, input.expiry); err == nil {
			t.Fatal("accepted invalid credential material")
		}
	}
}
