package hostlinux

import (
	"math"
	"testing"
)

func TestFilestoreCapacityArithmeticSaturates(t *testing.T) {
	if got := StatfsBytes(math.MaxUint64, 4096); got != math.MaxInt64 {
		t.Fatalf("StatfsBytes() = %d", got)
	}
	if got := SaturatingAdd(math.MaxInt64-1, 10); got != math.MaxInt64 {
		t.Fatalf("SaturatingAdd() = %d", got)
	}
	if got := SaturatingAdd(math.MinInt64+1, -10); got != math.MinInt64 {
		t.Fatalf("SaturatingAdd() lower bound = %d", got)
	}
	if got := SaturatingAdd(8, -3); got != 5 {
		t.Fatalf("SaturatingAdd() ordinary signed sum = %d", got)
	}
	if got := RemainingCapacity(100, 80, 30); got != 0 {
		t.Fatalf("RemainingCapacity() = %d", got)
	}
}
