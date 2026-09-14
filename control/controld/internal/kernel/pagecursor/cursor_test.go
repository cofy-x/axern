package pagecursor

import (
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestRoundTrip(t *testing.T) {
	createdAt := time.Date(2026, 9, 14, 12, 0, 0, 123, time.UTC)
	encoded := Encode(createdAt, "run-1")
	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !decoded.CreatedAt.Equal(createdAt) || decoded.ID != "run-1" {
		t.Fatalf("Decode(Encode()) = %+v", decoded)
	}
}

func TestDecodeRejectsMalformedCursor(t *testing.T) {
	for _, value := range []string{"not-base64!", "e30"} {
		if _, err := Decode(value); grpcstatus.Code(err) != codes.InvalidArgument {
			t.Fatalf("Decode(%q) code = %v, want InvalidArgument", value, grpcstatus.Code(err))
		}
	}
}

func TestPageSizeBounds(t *testing.T) {
	if PageSize(0) != DefaultSize || PageSize(MaxSize+1) != MaxSize || PageSize(7) != 7 {
		t.Fatal("page size bounds are incorrect")
	}
}
