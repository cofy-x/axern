package pgsecret

import (
	"crypto/rand"
	"fmt"
	"io"
)

func (s *Store) encrypt(plaintext []byte) ([]byte, error) {
	nonce := make([]byte, s.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("generate secret nonce: %w", err)
	}
	return s.aead.Seal(nonce, nonce, plaintext, nil), nil
}

func (s *Store) decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) < s.aead.NonceSize() {
		return nil, fmt.Errorf("encrypted secret payload is too short")
	}
	nonce := ciphertext[:s.aead.NonceSize()]
	payload := ciphertext[s.aead.NonceSize():]
	plaintext, err := s.aead.Open(nil, nonce, payload, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt secret payload: %w", err)
	}
	return plaintext, nil
}
