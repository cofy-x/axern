package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
	daemonprocess "github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/process"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/workload"
)

func TestServerHandlers(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	t.Setenv("AXERN_SANDBOXD_BROWSER_CMD", "")
	t.Setenv("AXERN_SANDBOXD_BROWSER_OPEN_CMD", "")
	t.Setenv("PATH", t.TempDir())

	state := workload.NewState("/tmp/sandboxd.sock")
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	server := New(state, daemonprocess.NewRegistry(waiter, nil, ""), waiter)

	assertStatus(t, server, "/healthz", http.StatusOK)
	assertStatus(t, server, "/readyz", http.StatusOK)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	server.ServeHTTP(resp, req)
	var ready wire.ReadyResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &ready); err != nil {
		t.Fatalf("decode ready: %v", err)
	}
	if ready.ProtocolVersion != wire.ProtocolVersion || !ready.Ready {
		t.Fatalf("ready = %#v", ready)
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/capabilities", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("capabilities status = %d", resp.Code)
	}
	var capabilities struct {
		ProtocolVersion int      `json:"protocolVersion"`
		Capabilities    []string `json:"capabilities"`
		Summary         struct {
			Total       int `json:"total"`
			Available   int `json:"available"`
			Unavailable int `json:"unavailable"`
		} `json:"summary"`
		Providers []struct {
			Name         string   `json:"name"`
			State        string   `json:"state"`
			Available    bool     `json:"available"`
			Capabilities []string `json:"capabilities"`
			Reason       string   `json:"reason"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &capabilities); err != nil {
		t.Fatalf("decode capabilities: %v", err)
	}
	if capabilities.ProtocolVersion != 1 {
		t.Fatalf("protocol version = %d, want 1", capabilities.ProtocolVersion)
	}
	if capabilities.Summary.Total == 0 || capabilities.Summary.Available == 0 {
		t.Fatalf("capability summary = %#v", capabilities.Summary)
	}
	want := map[string]bool{"health": true, "status": true, "supervisor": true, "diagnostics": true, "probe": true, "ports": true, "mounts": true, "file": true, "archive": true, "process": true, "pty": true}
	for _, got := range capabilities.Capabilities {
		delete(want, got)
	}
	if len(want) != 0 {
		t.Fatalf("missing capabilities: %#v", want)
	}
	providers := map[string]bool{}
	for _, item := range capabilities.Providers {
		if item.State == "" {
			t.Fatalf("provider state is empty: %#v", item)
		}
		providers[item.Name] = item.Available
	}
	if !providers["core"] || !providers["file"] || !providers["process"] {
		t.Fatalf("providers = %#v", capabilities.Providers)
	}
	if providers["computer_use"] {
		t.Fatalf("computer-use provider should not be available by default: %#v", capabilities.Providers)
	}
	if providers["browser"] {
		t.Fatalf("browser provider should not be available by default: %#v", capabilities.Providers)
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/status", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("status code = %d", resp.Code)
	}
	var status workload.StatusResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.UserProcess.State != workload.UserStateNotStarted {
		t.Fatalf("user state = %q, want %q", status.UserProcess.State, workload.UserStateNotStarted)
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/diagnostics", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("diagnostics code = %d, body = %s", resp.Code, resp.Body.String())
	}
	var diagnostics struct {
		ProtocolVersion int                     `json:"protocolVersion"`
		GeneratedAt     time.Time               `json:"generatedAt"`
		Ready           bool                    `json:"ready"`
		Status          workload.StatusResponse `json:"status"`
		Capabilities    []string                `json:"capabilities"`
		ProviderSummary struct {
			Total       int `json:"total"`
			Available   int `json:"available"`
			Unavailable int `json:"unavailable"`
		} `json:"providerSummary"`
		ProcessSummary struct {
			Total int `json:"total"`
		} `json:"processSummary"`
		Providers []struct {
			Name      string `json:"name"`
			Available bool   `json:"available"`
		} `json:"providers"`
		Processes struct {
			Processes []wire.ProcessStatus `json:"processes"`
		} `json:"processes"`
		ComputerUse struct {
			Available bool `json:"available"`
		} `json:"computerUse"`
		Browser struct {
			Available bool `json:"available"`
			Running   bool `json:"running"`
		} `json:"browser"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &diagnostics); err != nil {
		t.Fatalf("decode diagnostics: %v", err)
	}
	if diagnostics.ProtocolVersion != 1 || diagnostics.Status.UserProcess.State != workload.UserStateNotStarted {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
	if diagnostics.GeneratedAt.IsZero() {
		t.Fatalf("diagnostics generatedAt is zero: %#v", diagnostics)
	}
	if !diagnostics.Ready || diagnostics.ProviderSummary.Total == 0 || diagnostics.ProviderSummary.Available == 0 {
		t.Fatalf("diagnostics lifecycle summary = %#v", diagnostics)
	}
	if len(diagnostics.Capabilities) == 0 || len(diagnostics.Providers) == 0 {
		t.Fatalf("diagnostics missing provider details: %#v", diagnostics)
	}
	if diagnostics.ComputerUse.Available || diagnostics.Browser.Available || diagnostics.Browser.Running {
		t.Fatalf("diagnostics desktop status should be unavailable by default: %#v", diagnostics)
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/diagnostics?detail=full", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("full diagnostics code = %d, body = %s", resp.Code, resp.Body.String())
	}
	var full wire.DiagnosticsResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &full); err != nil {
		t.Fatalf("decode full diagnostics: %v", err)
	}
	if full.Detail != "full" || full.Ports == nil || full.Mounts == nil || full.FileLimits == nil || full.ComputerUse == nil || full.Browser == nil {
		t.Fatalf("full diagnostics = %#v", full)
	}
	if full.FileLimits.MaxArchiveEntries == 0 || full.FileLimits.MaxArchiveBytes == 0 {
		t.Fatalf("full diagnostics file limits = %#v", full.FileLimits)
	}
	computerUseProvider := findProvider(full.Providers, "computer_use")
	if computerUseProvider == nil || computerUseProvider.Available || len(computerUseProvider.Dependencies) == 0 {
		t.Fatalf("full diagnostics computer-use provider = %#v", computerUseProvider)
	}
	if diagnostics.Processes.Processes != nil {
		t.Fatalf("summary diagnostics should not include process list: %#v", diagnostics.Processes)
	}
}

func findProvider(items []wire.CapabilityProvider, name string) *wire.CapabilityProvider {
	for i := range items {
		if items[i].Name == name {
			return &items[i]
		}
	}
	return nil
}

func TestServerProbeRejectsAmbiguousRequest(t *testing.T) {
	server := New(workload.NewState("/tmp/sandboxd.sock"), nil, nil)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/probe", strings.NewReader(`{"http":{"port":80},"tcp":{"port":80}}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("probe status = %d, body = %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), "exactly one probe target is required") {
		t.Fatalf("probe body = %s", resp.Body.String())
	}
}

func TestServerFileHandlers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "message.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	server := New(workload.NewState("/tmp/sandboxd.sock"), nil, nil)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/files/stat?path="+path, nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("stat status = %d, body = %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/files/read?path="+path, nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"data":"aGVsbG8="`) {
		t.Fatalf("read response = status %d body %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/files/list?path="+dir, nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), "message.txt") {
		t.Fatalf("list response = status %d body %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/files/exists?path="+filepath.Join(dir, "missing"), nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"exists":false`) {
		t.Fatalf("exists response = status %d body %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/files/archive/upload?path="+dir, nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("archive upload GET status = %d, body = %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/files/archive/download?path="+dir, nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusMethodNotAllowed {
		t.Fatalf("archive download POST status = %d, body = %s", resp.Code, resp.Body.String())
	}
}

func TestServerFileMutationRejectsUnknownFields(t *testing.T) {
	server := New(workload.NewState("/tmp/sandboxd.sock"), nil, nil)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/files/write", strings.NewReader(`{"path":"/tmp/message.txt","data":"aGk=","unexpected":true}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("write status = %d, body = %s", resp.Code, resp.Body.String())
	}
	assertErrorResponse(t, resp, errorCodeInvalidArgument)
}

func TestReadyzReportsControlPlaneReadyWhileUserProcessStarting(t *testing.T) {
	state := workload.NewState("/tmp/sandboxd.sock")
	state.SetUserProcess(workload.UserProcessStatus{State: workload.UserStateStarting})
	server := New(state, nil, nil)
	assertStatus(t, server, "/readyz", http.StatusOK)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/diagnostics", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("diagnostics status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var diagnostics wire.DiagnosticsResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &diagnostics); err != nil {
		t.Fatalf("decode diagnostics: %v", err)
	}
	if !diagnostics.Ready || diagnostics.Status.UserProcess.State != workload.UserStateStarting {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestServerProcessHandlers(t *testing.T) {
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	server := New(workload.NewState("/tmp/sandboxd.sock"), daemonprocess.NewRegistry(waiter, nil, ""), waiter)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/processes", strings.NewReader(`{
		"args":["/bin/sh","-c","cat; printf ':done'"],
		"stdin":"ok",
		"captureOutput":true
	}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("start status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var started wire.ProcessStatus
	if err := json.Unmarshal(resp.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start: %v", err)
	}
	if started.ID == "" {
		t.Fatalf("started status = %#v", started)
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/processes", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var listed wire.ProcessListResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Processes) != 1 || listed.Processes[0].ID != started.ID {
		t.Fatalf("listed processes = %#v", listed)
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/diagnostics", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("diagnostics status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var diagnostics wire.DiagnosticsResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &diagnostics); err != nil {
		t.Fatalf("decode diagnostics: %v", err)
	}
	summarizedProcesses := diagnostics.ProcessSummary.Starting + diagnostics.ProcessSummary.Running + diagnostics.ProcessSummary.Exited + diagnostics.ProcessSummary.Failed
	if diagnostics.ProcessSummary.Total != 1 || summarizedProcesses != 1 {
		t.Fatalf("diagnostics process summary = %#v", diagnostics.ProcessSummary)
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/processes/"+started.ID+"/wait", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("wait status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var waited wire.ProcessStatus
	if err := json.Unmarshal(resp.Body.Bytes(), &waited); err != nil {
		t.Fatalf("decode wait: %v", err)
	}
	if waited.ExitCode == nil || *waited.ExitCode != 0 || waited.Stdout != "ok:done" {
		t.Fatalf("waited status = %#v", waited)
	}
}

func TestServerProcessStartRejectsUnknownFields(t *testing.T) {
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	server := New(workload.NewState("/tmp/sandboxd.sock"), daemonprocess.NewRegistry(waiter, nil, ""), waiter)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/processes", strings.NewReader(`{"args":["/bin/true"],"unexpected":true}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("start status = %d, body = %s", resp.Code, resp.Body.String())
	}
	assertErrorResponse(t, resp, errorCodeInvalidArgument)
}

func TestServerProcessControlRejectsUnknownFields(t *testing.T) {
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	server := New(workload.NewState("/tmp/sandboxd.sock"), daemonprocess.NewRegistry(waiter, nil, ""), waiter)

	for _, tt := range []struct {
		name string
		path string
		body string
	}{
		{name: "signal", path: "/processes/missing/signal", body: `{"signal":"TERM","unexpected":true}`},
		{name: "stdin", path: "/processes/missing/stdin", body: `{"data":"b2s=","unexpected":true}`},
		{name: "resize", path: "/processes/missing/resize", body: `{"cols":80,"rows":24,"unexpected":true}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, tt.path, strings.NewReader(tt.body))
			server.ServeHTTP(resp, req)
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("%s status = %d, body = %s", tt.path, resp.Code, resp.Body.String())
			}
			assertErrorResponse(t, resp, errorCodeInvalidArgument)
		})
	}
}

func TestServerProcessStreamHandlers(t *testing.T) {
	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	server := New(workload.NewState("/tmp/sandboxd.sock"), daemonprocess.NewRegistry(waiter, nil, ""), waiter)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/processes", strings.NewReader(`{
		"args":["/bin/sh","-c","cat"],
		"openStdin":true,
		"streamOutput":true
	}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("start status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var started wire.ProcessStatus
	if err := json.Unmarshal(resp.Body.Bytes(), &started); err != nil {
		t.Fatalf("decode start: %v", err)
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/processes/"+started.ID+"/stdin", strings.NewReader(`{"data":"b2s="}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("stdin status = %d, body = %s", resp.Code, resp.Body.String())
	}
	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/processes/"+started.ID+"/stdin-close", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("stdin close status = %d, body = %s", resp.Code, resp.Body.String())
	}
	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/processes/"+started.ID+"/wait", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("wait status = %d, body = %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/processes/"+started.ID+"/stream", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("stream status = %d, body = %s", resp.Code, resp.Body.String())
	}
	if !strings.Contains(resp.Body.String(), `"stdout":"b2s="`) {
		t.Fatalf("stream body = %s", resp.Body.String())
	}
}

func TestServerComputerUseHandlers(t *testing.T) {
	t.Setenv("DISPLAY", ":99")
	t.Setenv("AXERN_SANDBOXD_SCREENSHOT_CMD", "printf %s "+base64.StdEncoding.EncodeToString(testPNG())+" | base64 -d")
	t.Setenv("AXERN_SANDBOXD_DISPLAY_CMD", "printf '1280 720'")
	t.Setenv("AXERN_SANDBOXD_MOUSE_CMD", "true")
	t.Setenv("AXERN_SANDBOXD_KEYBOARD_CMD", "true")

	waiter := proc.NewWaiter(context.Background())
	defer waiter.Stop()
	server := New(workload.NewState("/tmp/sandboxd.sock"), nil, waiter)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/computer-use/status", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"available":true`) {
		t.Fatalf("status response = %d %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/capabilities", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"computer_use"`) {
		t.Fatalf("capabilities response = %d %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/computer-use/screenshot", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || resp.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("screenshot response = %d headers=%v body=%q", resp.Code, resp.Header(), resp.Body.Bytes())
	}
	if body := resp.Body.Bytes(); len(body) < 8 || string(body[:8]) != "\x89PNG\r\n\x1a\n" {
		t.Fatalf("screenshot body is not png: %q", resp.Body.Bytes())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/computer-use/mouse", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("mouse empty body status = %d, body = %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/computer-use/mouse", strings.NewReader(`{"action":"move","x":10,"y":20}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("mouse response = %d %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/computer-use/keyboard", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("keyboard empty body status = %d, body = %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/computer-use/keyboard", strings.NewReader(`{"key":"Escape"}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("keyboard response = %d %s", resp.Code, resp.Body.String())
	}
}

func TestServerComputerUseScreenshotUnavailable(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")

	server := New(workload.NewState("/tmp/sandboxd.sock"), nil, nil)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/computer-use/screenshot", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusServiceUnavailable {
		t.Fatalf("screenshot unavailable status = %d, body = %s", resp.Code, resp.Body.String())
	}
	assertErrorResponse(t, resp, errorCodeUnavailable)
}

func TestServerRejectsLooseJSONRequests(t *testing.T) {
	t.Setenv("DISPLAY", ":99")
	t.Setenv("AXERN_SANDBOXD_MOUSE_CMD", "true")

	server := New(workload.NewState("/tmp/sandboxd.sock"), nil, nil)
	for _, tt := range []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"action":"move","x":10,"y":20,"extra":true}`},
		{name: "trailing json", body: `{"action":"move","x":10,"y":20}{}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resp := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/computer-use/mouse", strings.NewReader(tt.body))
			server.ServeHTTP(resp, req)
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("mouse status = %d, body = %s", resp.Code, resp.Body.String())
			}
			assertErrorResponse(t, resp, errorCodeInvalidArgument)
		})
	}
}

func TestServerRejectsOversizedJSONRequest(t *testing.T) {
	t.Setenv("DISPLAY", ":99")
	t.Setenv("AXERN_SANDBOXD_MOUSE_CMD", "true")

	server := New(workload.NewState("/tmp/sandboxd.sock"), nil, nil)
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/computer-use/mouse", bytes.NewReader(bytes.Repeat([]byte("x"), maxJSONRequestBytes+1)))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("mouse status = %d, body = %s", resp.Code, resp.Body.String())
	}
	assertErrorResponse(t, resp, errorCodeInvalidArgument)
}

func TestServerBrowserHandlers(t *testing.T) {
	dir := t.TempDir()
	openPath := filepath.Join(dir, "open")
	closePath := filepath.Join(dir, "close")
	xdotoolLog := filepath.Join(dir, "xdotool.log")
	xdotoolPath := filepath.Join(dir, "xdotool")
	if err := os.WriteFile(xdotoolPath, []byte("#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$AXERN_XDOTOOL_LOG\"\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	t.Setenv("AXERN_XDOTOOL_LOG", xdotoolLog)
	t.Setenv("AXERN_SANDBOXD_BROWSER_OPEN_CMD", "printf '%s' \"$AXERN_BROWSER_URL\" >"+openPath)
	t.Setenv("AXERN_SANDBOXD_BROWSER_CLOSE_CMD", "printf closed >"+closePath)

	server := New(workload.NewState("/tmp/sandboxd.sock"), nil, nil)

	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/browser/status", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"available":true`) {
		t.Fatalf("browser status = %d %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/browser/open", strings.NewReader(`{"url":"https://example.com"}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"running":true`) {
		t.Fatalf("browser open = %d %s", resp.Code, resp.Body.String())
	}
	if got := string(mustReadFile(t, openPath)); got != "https://example.com" {
		t.Fatalf("open hook = %q", got)
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/browser/open", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"running":true`) {
		t.Fatalf("browser open without body = %d %s", resp.Code, resp.Body.String())
	}
	if got := string(mustReadFile(t, openPath)); got != "about:blank" {
		t.Fatalf("open hook without body = %q", got)
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/browser/navigate", strings.NewReader(`{"url":"https://example.org"}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"url":"https://example.org"`) {
		t.Fatalf("browser navigate = %d %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/browser/resize", strings.NewReader(`{"width":800,"height":600}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("browser resize = %d %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/browser/click", strings.NewReader(`{"x":10,"y":20}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("browser click = %d %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/browser/click", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("browser click empty body = %d %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/browser/type", strings.NewReader(`{"text":"hello","delayMs":1}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("browser type = %d %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/browser/type", strings.NewReader(`{}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("browser type empty text = %d %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/browser/wait", strings.NewReader(`{"timeoutMs":1}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("browser wait = %d %s", resp.Code, resp.Body.String())
	}
	if log := string(mustReadFile(t, xdotoolLog)); !strings.Contains(log, "windowsize 800 600") || !strings.Contains(log, "mousemove 10 20 click 1") || !strings.Contains(log, "type --delay 1 -- hello") {
		t.Fatalf("xdotool log = %q", log)
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/browser/open", strings.NewReader(`{"url":"javascript:alert(1)"}`))
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("browser open invalid URL = %d %s", resp.Code, resp.Body.String())
	}

	resp = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/browser/close", nil)
	server.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK || !strings.Contains(resp.Body.String(), `"running":false`) {
		t.Fatalf("browser close = %d %s", resp.Code, resp.Body.String())
	}
	if got := string(mustReadFile(t, closePath)); got != "closed" {
		t.Fatalf("close hook = %q", got)
	}
}

func assertStatus(t *testing.T, server *Server, path string, want int) {
	t.Helper()
	resp := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	server.ServeHTTP(resp, req)
	if resp.Code != want {
		t.Fatalf("%s status = %d, want %d", path, resp.Code, want)
	}
}

func assertErrorResponse(t *testing.T, resp *httptest.ResponseRecorder, wantCode string) {
	t.Helper()
	var body wire.ErrorResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error response %q: %v", resp.Body.String(), err)
	}
	if body.Error.Code != wantCode || body.Error.Message == "" {
		t.Fatalf("error response = %#v, want code %q with message", body, wantCode)
	}
	if got := resp.Header().Get("Content-Type"); !strings.Contains(got, "application/json") {
		t.Fatalf("error content-type = %q", got)
	}
}

func testPNG() []byte {
	data, _ := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+/p9sAAAAASUVORK5CYII=")
	return data
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
