package nodestate

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	bolt "go.etcd.io/bbolt"
	"google.golang.org/protobuf/proto"
)

const snapshotKey = "state"

type DB struct {
	db *bolt.DB
}

func Open(path string) (*DB, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create node state directory: %w", err)
	}
	if dir != "." {
		if err := os.Chmod(dir, 0o750); err != nil {
			return nil, fmt.Errorf("secure node state directory: %w", err)
		}
	}
	db, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 2 * time.Second})
	if err != nil {
		return nil, fmt.Errorf("open node state database: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("secure node state database: %w", err)
	}
	return &DB{db: db}, nil
}

func (s *DB) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *DB) SaveSnapshot(bucket string, value proto.Message) error {
	return s.PutRecord(bucket, snapshotKey, value)
}

func (s *DB) LoadSnapshot(bucket string, value proto.Message) error {
	return s.GetRecord(bucket, snapshotKey, value)
}

func (s *DB) PutRecord(bucket, key string, value proto.Message) error {
	if err := validateAddress(bucket, key); err != nil {
		return err
	}
	if value == nil {
		return fmt.Errorf("node state value is required: %w", errord.ErrInvalidArgument)
	}
	data, err := proto.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal node state %s/%s: %w", bucket, key, err)
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b, err := tx.CreateBucketIfNotExists([]byte(bucket))
		if err != nil {
			return err
		}
		return b.Put([]byte(key), data)
	})
}

func (s *DB) GetRecord(bucket, key string, value proto.Message) error {
	if err := validateAddress(bucket, key); err != nil {
		return err
	}
	if value == nil {
		return fmt.Errorf("node state destination is required: %w", errord.ErrInvalidArgument)
	}
	var data []byte
	err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return errord.ErrNotFound
		}
		stored := b.Get([]byte(key))
		if stored == nil {
			return errord.ErrNotFound
		}
		data = append(data, stored...)
		return nil
	})
	if err != nil {
		return err
	}
	if err := proto.Unmarshal(data, value); err != nil {
		return fmt.Errorf("decode node state %s/%s: %w", bucket, key, err)
	}
	return nil
}

func (s *DB) DeleteRecord(bucket, key string) error {
	if err := validateAddress(bucket, key); err != nil {
		return err
	}
	return s.db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		return b.Delete([]byte(key))
	})
}

func (s *DB) ForEachRecord(bucket string, visit func(key string, value []byte) error) error {
	if err := validateName("bucket", bucket); err != nil {
		return err
	}
	if visit == nil {
		return fmt.Errorf("node state visitor is required: %w", errord.ErrInvalidArgument)
	}
	type record struct {
		key   string
		value []byte
	}
	var records []record
	if err := s.db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}
		return b.ForEach(func(key, value []byte) error {
			if value == nil {
				return nil
			}
			records = append(records, record{key: string(key), value: append([]byte(nil), value...)})
			return nil
		})
	}); err != nil {
		return err
	}
	for _, record := range records {
		if err := visit(record.key, record.value); err != nil {
			return err
		}
	}
	return nil
}

func validateAddress(bucket, key string) error {
	return errors.Join(validateName("bucket", bucket), validateName("key", key))
}

func validateName(kind, value string) error {
	if value == "" {
		return fmt.Errorf("node state %s is required: %w", kind, errord.ErrInvalidArgument)
	}
	return nil
}
