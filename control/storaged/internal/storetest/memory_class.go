package storetest

import (
	"context"
	"fmt"
	"sort"

	storagev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/storage/v1"
	"google.golang.org/protobuf/proto"
)

func (s *MemoryStore) CreateVolumeClass(_ context.Context, class *storagev1.VolumeClass) (*storagev1.VolumeClass, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	if _, exists := s.classes[class.GetName()]; exists {
		return nil, fmt.Errorf("volume class %q already exists", class.GetName())
	}
	out := proto.Clone(class).(*storagev1.VolumeClass)
	s.classes[class.GetName()] = out
	return proto.Clone(out).(*storagev1.VolumeClass), nil
}

func (s *MemoryStore) GetVolumeClass(_ context.Context, name string) (*storagev1.VolumeClass, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	class, ok := s.classes[name]
	if !ok {
		return nil, false, nil
	}
	return proto.Clone(class).(*storagev1.VolumeClass), true, nil
}

func (s *MemoryStore) ListVolumeClasses(context.Context) ([]*storagev1.VolumeClass, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	names := make([]string, 0, len(s.classes))
	for name := range s.classes {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*storagev1.VolumeClass, 0, len(s.classes))
	for _, name := range names {
		out = append(out, proto.Clone(s.classes[name]).(*storagev1.VolumeClass))
	}
	return out, nil
}
