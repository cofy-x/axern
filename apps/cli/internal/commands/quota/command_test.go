package quota

import "testing"

func TestPersistentInodesAreExactFiniteIntegers(t *testing.T) {
	for _, text := range []string{"-1", "1.5", "9223372036854775808"} {
		if _, err := optionalInodes(text); err == nil {
			t.Fatalf("invalid inode bound accepted: %s", text)
		}
	}
	value, err := optionalInodes("9007199254740993")
	if err != nil || value.GetValue() != 9007199254740993 {
		t.Fatalf("inode precision lost: %v, %v", value, err)
	}
}
