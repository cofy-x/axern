package parse

import (
	"reflect"
	"strings"
	"testing"

	commonv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/common/v1"
	secretv1 "github.com/cofy-x/axern/sdk/go/gen/axern/control/secret/v1"
)

func TestSecretEnvVars(t *testing.T) {
	got, err := SecretEnvVars([]string{"TOKEN=sec-1:token"})
	if err != nil {
		t.Fatalf("SecretEnvVars returned error: %v", err)
	}
	want := []*commonv1.SecretEnvVar{{Name: "TOKEN", SecretID: "sec-1", Key: "token"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestSecretFiles(t *testing.T) {
	got, err := SecretFiles([]string{"/var/run/secret=sec-1:config:0440"})
	if err != nil {
		t.Fatalf("SecretFiles returned error: %v", err)
	}
	want := []*commonv1.SecretFile{{Path: "/var/run/secret", SecretID: "sec-1", Key: "config", Mode: 0o440}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestSecretType(t *testing.T) {
	tests := []struct {
		input   string
		want    secretv1.SecretType
		wantErr bool
	}{
		{input: "opaque", want: secretv1.SecretType_SECRET_TYPE_OPAQUE},
		{input: "docker-config-json", want: secretv1.SecretType_SECRET_TYPE_DOCKER_CONFIG_JSON},
		{input: "wat", wantErr: true},
	}
	for _, tc := range tests {
		got, err := SecretType(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Fatalf("expected error for %q", tc.input)
			}
			if !strings.Contains(err.Error(), "opaque, docker-config-json") {
				t.Fatalf("error %q does not include valid values", err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.input, err)
		}
		if got != tc.want {
			t.Fatalf("got %v, want %v", got, tc.want)
		}
	}
}
