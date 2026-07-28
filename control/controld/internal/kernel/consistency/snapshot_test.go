package consistencykernel

import "testing"

func TestNewSnapshotStatusAndTruncation(t *testing.T) {
	snapshot := NewSnapshot(Counts{ActiveReservations: 3}, []Issue{{
		Code:     IssueActiveReservationOnEndedAllocation,
		Severity: SeverityError,
	}}, true)

	if snapshot.Status != StatusInconsistent {
		t.Fatalf("status = %q, want inconsistent", snapshot.Status)
	}
	if snapshot.Counts.Issues != 1 {
		t.Fatalf("issues count = %d, want 1", snapshot.Counts.Issues)
	}
	if !snapshot.Truncated {
		t.Fatal("truncated = false, want true")
	}
}

func TestNewSnapshotNormalizesNilIssues(t *testing.T) {
	snapshot := NewSnapshot(Counts{}, nil, false)
	if snapshot.Status != StatusOK {
		t.Fatalf("status = %q, want ok", snapshot.Status)
	}
	if snapshot.Issues == nil {
		t.Fatal("issues = nil, want stable empty slice")
	}
}
