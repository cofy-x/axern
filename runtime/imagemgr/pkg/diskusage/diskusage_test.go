package diskusage

import "testing"

func TestUsedRatioByAvailable_Range(t *testing.T) {
	ratio, err := UsedRatioByAvailable(t.TempDir())
	if err != nil {
		t.Fatalf("UsedRatioByAvailable() error: %v", err)
	}
	if ratio < 0 || ratio > 1 {
		t.Fatalf("ratio out of range [0,1], got %f", ratio)
	}
}

func TestUsedPercentByFree_Range(t *testing.T) {
	percent, err := UsedPercentByFree(t.TempDir())
	if err != nil {
		t.Fatalf("UsedPercentByFree() error: %v", err)
	}
	if percent < 0 || percent > 100 {
		t.Fatalf("percent out of range [0,100], got %f", percent)
	}
}
