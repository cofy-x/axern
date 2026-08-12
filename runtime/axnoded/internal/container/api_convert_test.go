package container

import (
	"testing"
	"time"

	runtime "github.com/cofy-x/axern/runtime/axnoded/internal/apipb/v1"
	spec "github.com/opencontainers/runtime-spec/specs-go"
)

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		name      string
		timestamp string
		want      int64
	}{
		{"RFC3339 format", "2023-03-30T15:53:15.73829398+08:00", 1680162795},
		{"unix format", "1680162795", 1680162795},
		{"go string format with timezone", "2023-08-28 16:34:07.878055688 +0800 CST", 1693211647},
		{"go string format with monotonic suffix", "2023-08-28 16:34:07.878055688 +0800 CST m=+0.008551102", 1693211647},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseTimestamp(tt.timestamp); got != tt.want {
				t.Errorf("ParseTimestamp() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseTimestampTimePreservesNanoseconds(t *testing.T) {
	const raw = "2023-03-30T15:53:15.73829398+08:00"
	want, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := ParseTimestampTime(raw); !got.Equal(want) {
		t.Fatalf("ParseTimestampTime() = %s, want %s", got, want)
	}
	if got := ParseTimestampTime("malformed"); !got.IsZero() {
		t.Fatalf("ParseTimestampTime(malformed) = %s, want zero", got)
	}
}

func TestMountsToAPI(t *testing.T) {
	got := MountsToAPI([]spec.Mount{{
		Source:      "/host",
		Destination: "/data",
		Type:        "bind",
		Options:     []string{"ro"},
	}})
	if len(got) != 1 {
		t.Fatalf("len(MountsToAPI()) = %d, want 1", len(got))
	}
	want := &runtime.Mount{Source: "/host", Target: "/data", Type: "bind", Options: []string{"ro"}}
	if got[0].GetSource() != want.GetSource() || got[0].GetTarget() != want.GetTarget() || got[0].GetType() != want.GetType() || got[0].GetOptions()[0] != want.GetOptions()[0] {
		t.Fatalf("MountsToAPI()[0] = %#v, want %#v", got[0], want)
	}
}
