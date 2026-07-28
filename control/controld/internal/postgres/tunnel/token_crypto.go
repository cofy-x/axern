package pgtunnel

import (
	"crypto/rand"
	"fmt"
	"io"
)

func (s *Store) encryptNodeToken(token string) ([]byte, error) {
	if s.aead == nil {
		return []byte(token), nil
	}
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate tunnel token nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, []byte(token), nil), nil
}

func (s *Store) decryptNodeToken(ciphertext []byte) (string, error) {
	if s.aead == nil {
		return string(ciphertext), nil
	}
	if len(ciphertext) < s.aead.NonceSize() {
		return "", fmt.Errorf("encrypted tunnel node token is too short")
	}
	nonce := ciphertext[:s.aead.NonceSize()]
	payload := ciphertext[s.aead.NonceSize():]
	plaintext, err := s.aead.Open(nil, nonce, payload, nil)
	if err != nil {
		return "", fmt.Errorf("decrypt tunnel node token: %w", err)
	}
	return string(plaintext), nil
}
