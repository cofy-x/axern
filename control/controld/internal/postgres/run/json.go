package pgrun

import (
	"encoding/json"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func marshalProtoJSON(message proto.Message) (string, error) {
	payload, err := protojson.Marshal(message)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func marshalJSONMap(values map[string]string) (string, error) {
	if values == nil {
		values = map[string]string{}
	}
	payload, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func unmarshalJSONMap(payload []byte) map[string]string {
	out := map[string]string{}
	if len(payload) == 0 {
		return out
	}
	_ = json.Unmarshal(payload, &out)
	return out
}
