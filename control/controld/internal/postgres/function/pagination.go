package pgfunction

import (
	"encoding/base64"
	"strconv"
	"strings"

	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func pageSlice[T any](items []T, cursor string, pageSize int32) ([]T, string, error) {
	if pageSize <= 0 {
		return items, "", nil
	}
	offset, err := decodeOffsetCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	if offset >= len(items) {
		return []T{}, "", nil
	}
	end := offset + int(pageSize)
	if end >= len(items) {
		return items[offset:], "", nil
	}
	return items[offset:end], encodeOffsetCursor(end), nil
}

func encodeOffsetCursor(offset int) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.Itoa(offset)))
}

func decodeOffsetCursor(cursor string) (int, error) {
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return 0, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return 0, grpcstatus.Error(codes.InvalidArgument, "invalid function list cursor")
	}
	offset, err := strconv.Atoi(string(raw))
	if err != nil || offset < 0 {
		return 0, grpcstatus.Error(codes.InvalidArgument, "invalid function list cursor")
	}
	return offset, nil
}
