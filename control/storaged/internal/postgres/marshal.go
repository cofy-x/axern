package postgres

import (
	"encoding/json"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func marshalProto(msg proto.Message) ([]byte, error) {
	return protojson.MarshalOptions{UseProtoNames: true}.Marshal(msg)
}

func timestampOrNil(value *timestamppb.Timestamp) any {
	if value == nil {
		return nil
	}
	return value.AsTime().UTC()
}

func timestamp(value time.Time) *timestamppb.Timestamp {
	return timestamppb.New(value.UTC())
}

func marshalStringMap(values map[string]string) ([]byte, error) {
	if values == nil {
		values = map[string]string{}
	}
	return json.Marshal(values)
}
