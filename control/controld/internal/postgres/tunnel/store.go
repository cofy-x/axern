package pgtunnel

import (
	"crypto/aes"
	"crypto/cipher"
	"strings"
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
	db             *postgres.DB
	edgeTarget     string
	nodeEdgeTarget string
	relays         []Relay
	aead           cipher.AEAD
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

func NewStore(db *postgres.DB, edgeTarget, nodeEdgeTarget string, options ...Option) *Store {
	s := &Store{db: db, edgeTarget: strings.TrimSpace(edgeTarget), nodeEdgeTarget: strings.TrimSpace(nodeEdgeTarget)}
	for _, option := range options {
		if option != nil {
			option(s)
		}
	}
	return s
}
