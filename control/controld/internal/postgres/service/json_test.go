package pgservice

import (
	"testing"

	servicev1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/service/v1"
)

func TestMarshalProtoJSONPreservesAbsentValue(t *testing.T) {
	var probe *servicev1.ServiceProbe
	payload, err := marshalProtoJSON(probe)
	if err != nil {
		t.Fatalf("marshalProtoJSON(nil) error = %v", err)
	}
	if payload != "null" {
		t.Fatalf("marshalProtoJSON(nil) = %q, want null", payload)
	}
}
