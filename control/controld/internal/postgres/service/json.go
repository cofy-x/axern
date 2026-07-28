package pgservice

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func marshalProtoJSON(message proto.Message) (string, error) {
	if message == nil || !message.ProtoReflect().IsValid() {
		return "null", nil
	}
	payload, err := protojson.Marshal(message)
	if err != nil {
		return "", fmt.Errorf("marshal proto json: %w", err)
	}
	return string(payload), nil
}

func marshalJSONMap(values map[string]string) (string, error) {
	if len(values) == 0 {
		return "{}", nil
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("marshal json map: %w", err)
	}
	return string(payload), nil
}

func marshalStringSlice(values []string) (string, error) {
	if len(values) == 0 {
		return "[]", nil
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("marshal string slice: %w", err)
	}
	return string(payload), nil
}

func unmarshalJSONMap(payload []byte) map[string]string {
	if len(payload) == 0 {
		return nil
	}
	out := map[string]string{}
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func unmarshalStringSlice(payload []byte) []string {
	if len(payload) == 0 {
		return nil
	}
	var out []string
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
