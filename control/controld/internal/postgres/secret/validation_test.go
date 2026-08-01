package pgsecret

import (
	"strings"
	"testing"

	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

func TestNormalizeSecretDataPreservesValues(t *testing.T) {
	data, err := normalizeSecretData(secretv1.SecretType_SECRET_TYPE_OPAQUE, map[string]string{
		" token ": "  preserve me  ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := data["token"]; got != "  preserve me  " {
		t.Fatalf("token = %q, want value whitespace preserved", got)
	}
}

func TestNormalizeSecretDataRejectsOversizedPayload(t *testing.T) {
	_, err := normalizeSecretData(secretv1.SecretType_SECRET_TYPE_OPAQUE, map[string]string{
		"token": strings.Repeat("x", maxSecretPayloadBytes),
	})
	if grpcstatus.Code(err) != codes.InvalidArgument {
		t.Fatalf("error code = %v, want %v: %v", grpcstatus.Code(err), codes.InvalidArgument, err)
	}
}
