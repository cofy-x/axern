package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/runtime/contract"
)

func TestCapabilityVerificationRetriesOnlyInconclusiveResults(t *testing.T) {
	tests := []struct {
		name      string
		results   []contract.CapabilityVerification
		wantCalls int
		wantState contract.CapabilityVerificationState
	}{
		{name: "verified immediately", results: []contract.CapabilityVerification{contract.VerifiedCapability()}, wantCalls: 1, wantState: contract.CapabilityVerificationVerified},
		{name: "definitive loss immediately", results: []contract.CapabilityVerification{contract.LostCapability(errors.New("lost"))}, wantCalls: 1, wantState: contract.CapabilityVerificationLost},
		{name: "inconclusive then verified", results: []contract.CapabilityVerification{contract.InconclusiveCapability(errors.New("read")), contract.VerifiedCapability()}, wantCalls: 2, wantState: contract.CapabilityVerificationVerified},
		{name: "three inconclusive results", results: []contract.CapabilityVerification{contract.InconclusiveCapability(errors.New("one")), contract.InconclusiveCapability(errors.New("two")), contract.InconclusiveCapability(errors.New("three"))}, wantCalls: 3, wantState: contract.CapabilityVerificationInconclusive},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			got, err := verifyCapabilityWithDelays(context.Background(), []time.Duration{0, 0, 0}, func() contract.CapabilityVerification {
				result := tt.results[calls]
				calls++
				return result
			})
			if err != nil {
				t.Fatal(err)
			}
			if calls != tt.wantCalls || got.State != tt.wantState {
				t.Fatalf("calls=%d state=%d, want calls=%d state=%d", calls, got.State, tt.wantCalls, tt.wantState)
			}
		})
	}
}

func TestCapabilityVerificationStopsWhenContextIsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	_, err := verifyCapabilityWithDelays(ctx, []time.Duration{0, time.Hour}, func() contract.CapabilityVerification {
		calls++
		cancel()
		return contract.InconclusiveCapability(errors.New("read"))
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("error=%v calls=%d, want canceled after one call", err, calls)
	}
}
