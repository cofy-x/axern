package server

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	rolloutapp "github.com/cofy-x/axern/apps/axrun/internal/application/rollout"
	"github.com/cofy-x/axern/apps/axrun/internal/taskset"
)

func TestRolloutHTTPContract(t *testing.T) {
	bundle := buildHTTPTaskSet(t)
	output := t.TempDir()
	srv := New(Config{Output: output, AuthToken: "secret", MaxRollouts: 1}, rolloutapp.Service{})

	request := func(body, token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/rollouts", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		recorder := httptest.NewRecorder()
		srv.handleRollouts(recorder, req)
		return recorder
	}

	if got := request(`{"task_set_ref":"`+bundle+`","agent":"oracle","model":"m","execute":false}`, ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d", got.Code)
	}
	if got := request(`{"task_set_ref":"`+bundle+`","agent":"oracle","model":"m","unknown":true}`, "secret"); got.Code != http.StatusBadRequest {
		t.Fatalf("unknown field status = %d body=%s", got.Code, got.Body.String())
	}
	if got := request(`{"task_set_ref":"`+bundle+`","agent":"oracle","model":"m"}{}`, "secret"); got.Code != http.StatusBadRequest {
		t.Fatalf("multiple JSON status = %d body=%s", got.Code, got.Body.String())
	}
	got := request(`{"task_set_ref":"`+bundle+`","agent":"oracle","model":"m","execute":false}`, "secret")
	if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatalf("status=%d content-type=%q body=%s", got.Code, got.Header().Get("Content-Type"), got.Body.String())
	}
	if !strings.Contains(got.Body.String(), "event: run.accepted") || !strings.Contains(got.Body.String(), "event: run.completed") {
		t.Fatalf("SSE body = %s", got.Body.String())
	}
}

func buildHTTPTaskSet(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "input.txt"), []byte("input"), 0o644); err != nil {
		t.Fatal(err)
	}
	spec := `api_version: axrun/v1
kind: TaskSetBuild
metadata: {name: http}
spec:
  generators:
    - task_id: http-task
      instruction: {text: test}
      workspace: {paths: [input.txt], expand: aggregate}
      task:
        sandbox: {backend: local, workdir: /workspace}
        verifier: {type: none}
`
	buildFile := filepath.Join(root, "taskset.yaml")
	if err := os.WriteFile(buildFile, []byte(spec), 0o644); err != nil {
		t.Fatal(err)
	}
	bundle := filepath.Join(root, "bundle")
	if _, err := taskset.Build(taskset.BuildParams{File: buildFile, Output: bundle}); err != nil {
		t.Fatal(err)
	}
	return bundle
}
