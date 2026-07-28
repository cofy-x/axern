package pgfunction

import (
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func marshalProtoJSON(message proto.Message) (string, error) {
	if message == nil {
		return "{}", nil
	}
	payload, err := protojson.Marshal(message)
	if err != nil {
		return "", fmt.Errorf("marshal function proto json: %w", err)
	}
	return string(payload), nil
}

func marshalJSONMap(values map[string]string) (string, error) {
	if len(values) == 0 {
		return "{}", nil
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return "", fmt.Errorf("marshal function json map: %w", err)
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
