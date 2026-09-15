package main

import (
	"net/http"
	"strings"
)

func checkBaseline(client *http.Client) {
	mustStatus(client, http.MethodGet, "/healthz", nil, http.StatusOK, "status", "ok")
	mustStatus(client, http.MethodGet, "/readyz", nil, http.StatusOK, "ready", true)
	capabilities := mustObject(client, http.MethodGet, "/capabilities", nil, http.StatusOK)
	mustInt(capabilities, "protocolVersion", 1)
	requireCapabilities(capabilities, "archive", "diagnostics", "file", "health", "mounts", "ports", "probe", "process", "pty", "status", "supervisor")
	requireProvider(capabilities, "core", true)
	requireProvider(capabilities, "file", true)
	requireProvider(capabilities, "process", true)
	requireProvider(capabilities, "computer_use", false)
	requireProviderUnavailableDetail(capabilities, "computer_use")

	diagnostics := mustObject(client, http.MethodGet, "/diagnostics?detail=full", nil, http.StatusOK)
	mustInt(diagnostics, "protocolVersion", 1)
	mustBool(diagnostics, "ready", true)
	for _, key := range []string{"status", "providerSummary", "processSummary", "fileLimits", "ports", "mounts", "computerUse"} {
		if _, ok := diagnostics[key]; !ok {
			fail("diagnostics missing %s: %#v", key, diagnostics)
		}
	}
	mustProviderError(client, http.MethodGet, "/computer-use/screenshot", nil, http.StatusServiceUnavailable, "unavailable")
	mustProviderError(client, http.MethodPost, "/processes", []byte(`{}`), http.StatusBadRequest, "invalid_argument")
	mustProviderError(client, http.MethodGet, "/processes/proc-missing", nil, http.StatusNotFound, "not_found")
	mustProviderError(client, http.MethodGet, "/files/archive/upload", nil, http.StatusMethodNotAllowed, "method_not_allowed")
	mustProviderError(client, http.MethodGet, "/files/read?path=/tmp/axern-provider-missing", nil, http.StatusNotFound, "not_found")

	started := mustObject(client, http.MethodPost, "/processes", []byte(`{"args":["/bin/sh","-c","printf proc-ok"],"captureOutput":true}`), http.StatusCreated)
	id := mustString(started, "id")
	waited := mustObject(client, http.MethodGet, "/processes/"+id+"/wait", nil, http.StatusOK)
	mustInt(waited, "exitCode", 0)
	mustStringValue(waited, "stdout", "proc-ok")

	mustStatus(client, http.MethodPost, "/files/write", []byte(`{"path":"/tmp/axern-provider-file.txt","data":"ZmlsZS1vaw=="}`), http.StatusOK, "ok", true)
	read := mustObject(client, http.MethodGet, "/files/read?path=/tmp/axern-provider-file.txt", nil, http.StatusOK)
	mustStringValue(read, "data", "ZmlsZS1vaw==")
}

func checkOptional(client *http.Client, mousePath, keyboardPath string) {
	if strings.TrimSpace(mousePath) == "" || strings.TrimSpace(keyboardPath) == "" {
		fail("optional mode requires --mouse-file and --keyboard-file")
	}
	capabilities := mustObject(client, http.MethodGet, "/capabilities", nil, http.StatusOK)
	requireCapabilities(capabilities, "computer_use")
	requireProvider(capabilities, "computer_use", true)

	diagnostics := mustObject(client, http.MethodGet, "/diagnostics?detail=full", nil, http.StatusOK)
	if diagnostics["computerUse"] == nil {
		fail("optional diagnostics missing computer-use provider: %#v", diagnostics)
	}

	status := mustObject(client, http.MethodGet, "/computer-use/status", nil, http.StatusOK)
	mustBool(status, "available", true)
	display := mustObject(client, http.MethodGet, "/computer-use/display", nil, http.StatusOK)
	mustInt(display, "width", 1280)
	mustInt(display, "height", 720)
	screenshot := request(client, http.MethodGet, "/computer-use/screenshot", nil)
	if screenshot.status != http.StatusOK || len(screenshot.body) < 8 || string(screenshot.body[:8]) != "\x89PNG\r\n\x1a\n" {
		fail("screenshot status=%d body-prefix=%q", screenshot.status, string(screenshot.body))
	}
	mustObject(client, http.MethodPost, "/computer-use/mouse", []byte(`{"x":7,"y":9,"button":"1"}`), http.StatusOK)
	mustObject(client, http.MethodPost, "/computer-use/keyboard", []byte(`{"text":"hello"}`), http.StatusOK)
	mustFile(mousePath, "7:9:1:click")
	mustFile(keyboardPath, "hello:")

	requireProviderStatusAgrees(capabilities, "computer_use", status)
}
