package sshapi

import (
	"fmt"
	"os"

	gossh "golang.org/x/crypto/ssh"
)

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
