package storage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	runtimevolumev1 "github.com/cofy-x/axern/sdk/go/gen/axern/private/runtime/volume/v1"
	"google.golang.org/protobuf/encoding/protojson"
)

type Store interface {
	Load(context.Context) (map[string][]*runtimevolumev1.PublishedVolume, error)
	Save(context.Context, map[string][]*runtimevolumev1.PublishedVolume) error
}

type JSONStore struct {
	path string
}

func NewJSONStore(root string) *JSONStore {
	return &JSONStore{path: filepath.Join(root, "published_volumes.json")}
}

func (s *JSONStore) Load(ctx context.Context) (map[string][]*runtimevolumev1.PublishedVolume, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := map[string][]*runtimevolumev1.PublishedVolume{}
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read published volume state: %w", err)
	}
	var raw map[string][]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("decode published volume state: %w", err)
	}
	for allocationID, items := range raw {
		allocationID = strings.TrimSpace(allocationID)
		if allocationID == "" {
			return nil, fmt.Errorf("decode published volume state: allocation id is required")
		}
		for _, itemData := range items {
			item := &runtimevolumev1.PublishedVolume{}
			if err := protojson.Unmarshal(itemData, item); err != nil {
				return nil, fmt.Errorf("decode published volume %s: %w", allocationID, err)
			}
			if err := validatePublishedRecord(allocationID, item); err != nil {
				return nil, fmt.Errorf("decode published volume %s: %w", allocationID, err)
			}
			out[allocationID] = append(out[allocationID], item)
		}
	}
	return out, nil
}

func (s *JSONStore) Save(ctx context.Context, in map[string][]*runtimevolumev1.PublishedVolume) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create volume state directory: %w", err)
	}
	raw := make(map[string][]json.RawMessage, len(in))
	for allocationID, items := range in {
		allocationID = strings.TrimSpace(allocationID)
		if allocationID == "" {
			return fmt.Errorf("encode published volume state: allocation id is required")
		}
		for _, item := range items {
			if item == nil {
				return fmt.Errorf("encode published volume %s: published volume is required", allocationID)
			}
			item = clonePublished(item)
			if err := validatePublishedRecord(allocationID, item); err != nil {
				return fmt.Errorf("encode published volume %s: %w", allocationID, err)
			}
			data, err := protojson.Marshal(item)
			if err != nil {
				return fmt.Errorf("encode published volume %s: %w", allocationID, err)
			}
			raw[allocationID] = append(raw[allocationID], data)
		}
	}
	data, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return fmt.Errorf("encode published volume state: %w", err)
	}
	tmp, err := os.CreateTemp(dir, filepath.Base(s.path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary published volume state: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write published volume state: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync published volume state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close published volume state: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("chmod published volume state: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace published volume state: %w", err)
	}
	syncDir(dir)
	return nil
}

func syncDir(path string) {
	dir, err := os.Open(path)
	if err != nil {
		return
	}
	defer dir.Close()
	_ = dir.Sync()
}
