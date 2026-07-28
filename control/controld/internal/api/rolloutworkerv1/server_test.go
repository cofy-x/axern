package rolloutworkerv1

import "testing"

func TestEqualToken(t *testing.T) {
	if !equalToken("secret", "secret") {
		t.Fatal("equal token rejected")
	}
	for _, test := range [][2]string{{"", ""}, {"secret", ""}, {"", "secret"}, {"secret", "other"}} {
		if equalToken(test[0], test[1]) {
			t.Fatalf("equalToken(%q,%q) accepted", test[0], test[1])
		}
	}
}
