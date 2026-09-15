package pgtunnel

import (
	"crypto/aes"
	"crypto/cipher"
	"time"

	"github.com/cofy-x/axern/control/controld/internal/postgres"
)

const (
	defaultSessionTTL = 30 * time.Minute
	minSessionTTL     = time.Minute
	maxSessionTTL     = 24 * time.Hour
	autoPortMin       = 20000
	autoPortMax       = 59999
)

type Store struct {
	db      *postgres.DB
	relays  []Relay
	aead    cipher.AEAD
	watches *watchHub
}

type Option func(*Store)

func WithRelays(relays []Relay) Option {
	return func(s *Store) {
		s.relays = normalizeRelays(relays)
	}
}

func WithMasterKey(masterKey []byte) Option {
	return func(s *Store) {
		if len(masterKey) == 0 {
			return
		}
		block, err := aes.NewCipher(masterKey)
		if err != nil {
			return
		}
		aead, err := cipher.NewGCM(block)
		if err != nil {
			return
		}
		s.aead = aead
	}
}

func NewStore(db *postgres.DB, options ...Option) *Store {
	s := &Store{db: db}
	if db != nil && db.Pool() != nil {
		s.watches = newWatchHub(db.Pool())
	}
	for _, option := range options {
		if option != nil {
			option(s)
		}
	}
	return s
}

func (s *Store) Close() {
	if s != nil && s.watches != nil {
		s.watches.close()
	}
}
