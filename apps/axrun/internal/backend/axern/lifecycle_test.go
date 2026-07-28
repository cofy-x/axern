package axern

import (
	"errors"
	"testing"

	"github.com/cofy-x/axern/apps/axrun/internal/domain"
)

func TestRunnerExecuteStartErrorReturnsInfrastructureError(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "shell", Command: "true"})
	startErr := errors.New("start failed")
	_, err := (Adapter{Runtime: &fakeRuntime{err: startErr}, Now: fixedNow}).Execute(executeRequest(store, layout))
	if !errors.Is(err, startErr) {
		t.Fatalf("Execute error = %v, want %v", err, startErr)
	}
	var verifier domain.VerifierResult
	readJSON(t, layout.VerifierJSONPath, &verifier)
	if verifier.Status != domain.EpisodeStatusPending {
		t.Fatalf("verifier = %#v", verifier)
	}
}

func TestRunnerExecuteCloseErrorSurfacedWhenNoEarlierError(t *testing.T) {
	store, layout := createLayout(t, domain.VerifierSpec{Type: "none"})
	closeErr := errors.New("close failed")
	_, err := (Adapter{Runtime: &fakeRuntime{sandbox: &fakeSandbox{closeErr: closeErr}}, Now: fixedNow}).Execute(executeRequest(store, layout))
	if !errors.Is(err, closeErr) {
		t.Fatalf("Execute error = %v, want %v", err, closeErr)
	}
}
