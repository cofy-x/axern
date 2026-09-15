package adminkernel

import (
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestValidateAdmitNodeRequest(t *testing.T) {
	valid := AdmitNodeRequest{
		NodeID:          "node-a",
		EnrollmentToken: "0123456789abcdef0123456789abcdef",
		OperatorReason:  "add worker", Now: time.Now().UTC(),
	}
	if err := ValidateAdmitNodeRequest(valid); err != nil {
		t.Fatalf("ValidateAdmitNodeRequest(valid) error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*AdmitNodeRequest)
	}{
		{name: "node id", mutate: func(req *AdmitNodeRequest) { req.NodeID = "" }},
		{name: "credential", mutate: func(req *AdmitNodeRequest) { req.EnrollmentToken = "short" }},
		{name: "reason", mutate: func(req *AdmitNodeRequest) { req.OperatorReason = "" }},
		{name: "time", mutate: func(req *AdmitNodeRequest) { req.Now = time.Time{} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := valid
			test.mutate(&req)
			if err := ValidateAdmitNodeRequest(req); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("ValidateAdmitNodeRequest() error = %v, want InvalidArgument", err)
			}
		})
	}
}

func TestAdmissionRejectsUnrepresentableNodeIdentity(t *testing.T) {
	for _, id := range []string{"../node", "node/alias", " node", ".", ".."} {
		if err := ValidateAdmitNodeRequest(AdmitNodeRequest{NodeID: id, EnrollmentToken: "01234567890123456789012345678901", OperatorReason: "test", Now: time.Now()}); status.Code(err) != codes.InvalidArgument {
			t.Fatalf("invalid identity %q: %v", id, err)
		}
	}
}
