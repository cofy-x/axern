package control

import (
	"fmt"

	"google.golang.org/protobuf/proto"
)

type rawMessage []byte

type rawCodec struct{}

func (rawCodec) Name() string {
	return "proto"
}

func (rawCodec) Marshal(v any) ([]byte, error) {
	switch msg := v.(type) {
	case rawMessage:
		return []byte(msg), nil
	case *rawMessage:
		if msg == nil {
			return nil, nil
		}
		return []byte(*msg), nil
	case proto.Message:
		return proto.Marshal(msg)
	default:
		return nil, fmt.Errorf("control proxy raw codec cannot marshal %T", v)
	}
}

func (rawCodec) Unmarshal(data []byte, v any) error {
	switch msg := v.(type) {
	case *rawMessage:
		*msg = append((*msg)[:0], data...)
		return nil
	case proto.Message:
		return proto.Unmarshal(data, msg)
	default:
		return fmt.Errorf("control proxy raw codec cannot unmarshal into %T", v)
	}
}
