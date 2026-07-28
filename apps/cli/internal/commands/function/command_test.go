package function

import "testing"

func TestIDOrName(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		forceName bool
		wantID    string
		wantName  string
	}{
		{name: "resource id", value: "fn-123", wantID: "fn-123"},
		{name: "ordinary name", value: "hello", wantName: "hello"},
		{name: "prefixed name when namespace is explicit", value: "fn-worker", forceName: true, wantName: "fn-worker"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotID, gotName := idOrName(tt.value, "default", tt.forceName)
			if gotID != tt.wantID || gotName != tt.wantName {
				t.Fatalf("idOrName() = (%q, %q), want (%q, %q)", gotID, gotName, tt.wantID, tt.wantName)
			}
		})
	}
}
