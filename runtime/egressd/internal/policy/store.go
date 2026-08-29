package policy

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	runtimeegressv1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/egress/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

type Store interface {
	Load(context.Context) ([]*runtimeegressv1.PreparedEgressPolicy, error)
	Save(context.Context, []*runtimeegressv1.PreparedEgressPolicy) error
}

type JSONStore struct {
	path string
}

func NewJSONStore(root string) *JSONStore {
	return &JSONStore{path: filepath.Join(root, "prepared_policies.json")}
}

func (s *JSONStore) Load(ctx context.Context) ([]*runtimeegressv1.PreparedEgressPolicy, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read prepared egress policy state: %w", err)
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode prepared egress policy state: %w", err)
	}
	out := make([]*runtimeegressv1.PreparedEgressPolicy, 0, len(raw))
	for _, itemData := range raw {
		item := &runtimeegressv1.PreparedEgressPolicy{}
		if err := protojson.Unmarshal(itemData, item); err != nil {
			return nil, fmt.Errorf("decode prepared egress policy: %w", err)
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *JSONStore) Save(ctx context.Context, records []*runtimeegressv1.PreparedEgressPolicy) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create egress policy state directory: %w", err)
	}
	raw := make([]json.RawMessage, 0, len(records))
	for _, record := range records {
		data, err := protojson.Marshal(record)
		if err != nil {
			return fmt.Errorf("encode prepared egress policy: %w", err)
		}
		raw = append(raw, data)
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("encode prepared egress policy state: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary egress policy state: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write egress policy state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync egress policy state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close egress policy state: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("chmod egress policy state: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace egress policy state: %w", err)
	}
	directory, err := os.Open(dir)
	if err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}
