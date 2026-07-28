package sshapi

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	gossh "golang.org/x/crypto/ssh"
)

type AuthorizedKeys struct {
	keys map[string]struct{}
}

func LoadHostKey(path string) (gossh.Signer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ssh host key: %w", err)
	}
	signer, err := gossh.ParsePrivateKey(data)
	if err != nil {
		return nil, fmt.Errorf("parse ssh host key: %w", err)
	}
	return signer, nil
}

func LoadAuthorizedKeys(path string) (*AuthorizedKeys, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read ssh authorized_keys: %w", err)
	}
	keys, err := ParseAuthorizedKeys(data)
	if err != nil {
		return nil, err
	}
	return keys, nil
}

func ParseAuthorizedKeys(data []byte) (*AuthorizedKeys, error) {
	out := &AuthorizedKeys{keys: map[string]struct{}{}}
	for _, line := range bytes.Split(data, []byte("\n")) {
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || strings.HasPrefix(string(trimmed), "#") {
			continue
		}
		pub, _, _, _, err := gossh.ParseAuthorizedKey(trimmed)
		if err != nil {
			return nil, fmt.Errorf("parse ssh authorized_keys: %w", err)
		}
		out.keys[string(pub.Marshal())] = struct{}{}
	}
	if len(out.keys) == 0 {
		return nil, fmt.Errorf("parse ssh authorized_keys: no keys found")
	}
	return out, nil
}

func (a *AuthorizedKeys) Contains(key gossh.PublicKey) bool {
	if a == nil || key == nil {
		return false
	}
	_, ok := a.keys[string(key.Marshal())]
	return ok
}
