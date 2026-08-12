package storetest

import (
	"bytes"
	"fmt"
	"strings"
	"sync"

	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
	"google.golang.org/protobuf/proto"
)

func NewMockStore() *MockStore {
	return &MockStore{data: make(map[string]map[string][]byte)}
}

type MockStore struct {
	data map[string]map[string][]byte
	sync.Mutex
}

func (m *MockStore) SaveSnapshot(bucket string, value proto.Message) error {
	return m.PutRecord(bucket, "state", value)
}

func (m *MockStore) LoadSnapshot(bucket string, value proto.Message) error {
	return m.GetRecord(bucket, "state", value)
}

func (m *MockStore) PutRecord(bucket, key string, value proto.Message) error {
	m.Lock()
	defer m.Unlock()
	if strings.Contains(fmt.Sprint(value), "failed") {
		return errord.ErrInvalidArgument
	}
	data, err := proto.Marshal(value)
	if err != nil {
		return err
	}
	if m.data[bucket] == nil {
		m.data[bucket] = make(map[string][]byte)
	}
	m.data[bucket][key] = data
	return nil
}

func (m *MockStore) GetRecord(bucket, key string, value proto.Message) error {
	m.Lock()
	defer m.Unlock()
	data, ok := m.data[bucket][key]
	if !ok {
		return errord.ErrNotFound
	}
	return proto.Unmarshal(data, value)
}

func (m *MockStore) GetRecordBytes(bucket, key string) ([]byte, error) {
	m.Lock()
	defer m.Unlock()
	data, ok := m.data[bucket][key]
	if !ok {
		return nil, errord.ErrNotFound
	}
	return append([]byte(nil), data...), nil
}

func (m *MockStore) CompareAndSwapRecord(bucket, key string, expected []byte, expectedExists bool, next proto.Message) (bool, error) {
	m.Lock()
	defer m.Unlock()
	current, currentExists := m.data[bucket][key]
	if currentExists != expectedExists || (currentExists && !bytes.Equal(current, expected)) {
		return false, nil
	}
	if next == nil {
		delete(m.data[bucket], key)
		return true, nil
	}
	data, err := proto.Marshal(next)
	if err != nil {
		return false, err
	}
	if m.data[bucket] == nil {
		m.data[bucket] = make(map[string][]byte)
	}
	m.data[bucket][key] = data
	return true, nil
}

func (m *MockStore) DeleteRecord(bucket, key string) error {
	m.Lock()
	defer m.Unlock()
	delete(m.data[bucket], key)
	return nil
}

func (m *MockStore) ForEachRecord(bucket string, visit func(key string, value []byte) error) error {
	m.Lock()
	items := make(map[string][]byte, len(m.data[bucket]))
	for key, value := range m.data[bucket] {
		items[key] = append([]byte(nil), value...)
	}
	m.Unlock()
	for key, value := range items {
		if err := visit(key, value); err != nil {
			return err
		}
	}
	return nil
}

func (m *MockStore) Close() error { return nil }
