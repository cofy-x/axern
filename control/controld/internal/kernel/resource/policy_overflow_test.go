package resource

import (
	"math"
	"testing"
)

func TestAdmissionArithmeticSaturates(t *testing.T) {
	if got := SaturatingAdd(math.MaxInt64-1, 10); got != math.MaxInt64 {
		t.Fatalf("SaturatingAdd() = %d", got)
	}
	if got := SaturatingAdd(math.MinInt64+1, -10); got != math.MinInt64 {
		t.Fatalf("SaturatingAdd() lower bound = %d", got)
	}
	if got := SaturatingAdd(8, -3); got != 5 {
		t.Fatalf("SaturatingAdd() ordinary signed sum = %d", got)
	}
}
