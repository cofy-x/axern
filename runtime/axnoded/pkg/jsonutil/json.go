package jsonutil

import (
	"bytes"
	"encoding/json"
)

func UnescapedMarshal(in interface{}) ([]byte, error) {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(in); err != nil {
		return nil, err
	}

	result := b.Bytes()
	return result[:len(result)-1], nil
}
