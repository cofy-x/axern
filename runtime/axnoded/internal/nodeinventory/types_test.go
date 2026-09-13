package nodeinventory

import (
	"encoding/json"
	"testing"

	runtimeapi "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	"github.com/cofy-x/axern/runtime/axnoded/internal/environmentcache"
	capabilityv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/capability/v1"
	"google.golang.org/protobuf/proto"
)

func TestNewSnapshotInitializesHeatCollections(t *testing.T) {
	snapshot := NewSnapshot()
	if snapshot.Version != SnapshotVersion {
		t.Fatalf("snapshot version = %q, want %q", snapshot.Version, SnapshotVersion)
	}
	if snapshot.Heat.MountedImageURLs == nil {
		t.Fatal("expected mounted_image_urls to be initialized to an empty slice")
	}
	if len(snapshot.Heat.MountedImageURLs) != 0 {
		t.Fatalf("expected mounted_image_urls to start empty, got %d entries", len(snapshot.Heat.MountedImageURLs))
	}
	if snapshot.Heat.Locality == nil {
		t.Fatal("expected heat.locality to be initialized to an empty slice")
	}
	if len(snapshot.Heat.Locality) != 0 {
		t.Fatalf("expected heat.locality to start empty, got %d entries", len(snapshot.Heat.Locality))
	}
}

func TestNodeInfoJSONRoundTripsCapabilityOneof(t *testing.T) {
	want := NodeInfo{CapabilitySnapshot: &capabilityv1.CapabilitySnapshot{
		NodeInstanceID: "instance-a",
		Sequence:       3,
		Observations: []*capabilityv1.CapabilityObservation{{
			Key: &capabilityv1.CapabilityKey{Kind: &capabilityv1.CapabilityKey_Platform{
				Platform: capabilityv1.PlatformCapability_PLATFORM_CAPABILITY_RUNSC_MEMORY_HARD_LIMIT,
			}},
		}},
	}}
	payload, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	var got NodeInfo
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("Unmarshal() error = %v; payload = %s", err, payload)
	}
	if !proto.Equal(got.CapabilitySnapshot, want.CapabilitySnapshot) {
		t.Fatalf("capability snapshot = %v, want %v", got.CapabilitySnapshot, want.CapabilitySnapshot)
	}
}

func TestLocalityKeyFromRootfsConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  environmentcache.RootfsConfig
		want string
		ok   bool
	}{
		{
			name: "local",
			cfg: environmentcache.RootfsConfig{
				SrcType: runtimeapi.RootfsSrcType_LOCAL,
				Path:    "/tmp/../opt/rootfs",
			},
			want: "local:/opt/rootfs",
			ok:   true,
		},
		{
			name: "image",
			cfg: environmentcache.RootfsConfig{
				SrcType:  runtimeapi.RootfsSrcType_IMAGE,
				ImageUrl: "docker.io/library/nginx:latest",
			},
			want: "image:docker.io/library/nginx:latest",
			ok:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := LocalityKeyFromRootfsConfig(tt.cfg)
			if ok != tt.ok || got != tt.want {
				t.Fatalf("LocalityKeyFromRootfsConfig() = (%q, %v), want (%q, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}
