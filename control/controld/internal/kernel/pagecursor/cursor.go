package pagecursor

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"time"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

const (
	DefaultSize = 100
	MaxSize     = 500
)

type Cursor struct {
	CreatedAt time.Time `json:"created_at"`
	ID        string    `json:"id"`
}

func PageSize(requested int32) int {
	if requested <= 0 {
		return DefaultSize
	}
	if requested > MaxSize {
		return MaxSize
	}
	return int(requested)
}

func Encode(createdAt time.Time, id string) string {
	payload, _ := json.Marshal(Cursor{CreatedAt: createdAt.UTC(), ID: strings.TrimSpace(id)})
	return base64.RawURLEncoding.EncodeToString(payload)
}

func Decode(value string) (Cursor, error) {
	if strings.TrimSpace(value) == "" {
		return Cursor{}, nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(value))
	if err != nil {
		return Cursor{}, grpcstatus.Errorf(codes.InvalidArgument, "invalid cursor encoding: %v", err)
	}
	var cursor Cursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return Cursor{}, grpcstatus.Errorf(codes.InvalidArgument, "invalid cursor payload: %v", err)
	}
	if cursor.CreatedAt.IsZero() || strings.TrimSpace(cursor.ID) == "" {
		return Cursor{}, grpcstatus.Error(codes.InvalidArgument, "cursor is missing created_at or id")
	}
	cursor.CreatedAt = cursor.CreatedAt.UTC()
	cursor.ID = strings.TrimSpace(cursor.ID)
	return cursor, nil
}
