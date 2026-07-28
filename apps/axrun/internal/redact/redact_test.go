package redact

import (
	"strings"
	"testing"
)

func TestStringRedactsCommonSecretShapes(t *testing.T) {
	input := `Authorization: Bearer sk-test-secret url=https://example.test?api_key=sk-test-secret password=secret-value {"session_token":"secret-value"}`
	got := String(input)
	for _, forbidden := range []string{"sk-test-secret", "secret-value"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("String() leaked %q in %q", forbidden, got)
		}
	}
	if !strings.Contains(got, Replacement) {
		t.Fatalf("String() = %q, want replacement marker", got)
	}
}

func TestCommandRedactsFlagAndAssignmentValues(t *testing.T) {
	got := Command([]string{"tool", "--api-key", "sk-test-secret", "TOKEN=secret-value", "--model", "glm"})
	joined := strings.Join(got, " ")
	for _, forbidden := range []string{"sk-test-secret", "secret-value"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("Command() leaked %q in %#v", forbidden, got)
		}
	}
	if got[2] != Replacement || got[3] != "TOKEN="+Replacement || got[5] != "glm" {
		t.Fatalf("Command() = %#v", got)
	}
}

func TestHeaderDropsSensitiveHeaders(t *testing.T) {
	got := Header(map[string][]string{
		"Authorization": {"Bearer sk-test-secret"},
		"Content-Type":  {"application/json"},
		"X-Request-ID":  {"req-1"},
	})
	if _, ok := got["Authorization"]; ok {
		t.Fatalf("Header() kept Authorization: %#v", got)
	}
	if got["Content-Type"][0] != "application/json" || got["X-Request-ID"][0] != "req-1" {
		t.Fatalf("Header() = %#v", got)
	}
}
