package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

func unixClient(socketPath string) *http.Client {
	return &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				var dialer net.Dialer
				return dialer.DialContext(ctx, "unix", socketPath)
			},
		},
	}
}

func mustObject(client *http.Client, method, path string, body []byte, status int) map[string]any {
	result := request(client, method, path, body)
	if result.status != status {
		fail("%s %s status=%d want=%d body=%s", method, path, result.status, status, string(result.body))
	}
	var out map[string]any
	if err := json.Unmarshal(result.body, &out); err != nil {
		fail("%s %s invalid json: %v body=%s", method, path, err, string(result.body))
	}
	return out
}

func mustStatus(client *http.Client, method, path string, body []byte, status int, key string, want any) {
	object := mustObject(client, method, path, body, status)
	switch typed := want.(type) {
	case bool:
		mustBool(object, key, typed)
	case string:
		mustStringValue(object, key, typed)
	default:
		fail("unsupported status assertion value %#v", want)
	}
}

func mustProviderError(client *http.Client, method, path string, body []byte, status int, code string) {
	object := mustObject(client, method, path, body, status)
	errorObject, ok := object["error"].(map[string]any)
	if !ok {
		fail("%s %s missing error object: %#v", method, path, object)
	}
	mustStringValue(errorObject, "code", code)
	if strings.TrimSpace(mustString(errorObject, "message")) == "" {
		fail("%s %s empty error message: %#v", method, path, object)
	}
}

func request(client *http.Client, method, path string, body []byte) httpResult {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, "http://unix"+path, reader)
	if err != nil {
		fail("new request %s %s: %v", method, path, err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		fail("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		fail("read response %s %s: %v", method, path, err)
	}
	return httpResult{status: resp.StatusCode, body: data}
}

func requireCapabilities(object map[string]any, names ...string) {
	raw, ok := object["capabilities"].([]any)
	if !ok {
		fail("missing capabilities: %#v", object)
	}
	set := map[string]bool{}
	for _, item := range raw {
		set[fmt.Sprint(item)] = true
	}
	for _, name := range names {
		if !set[name] {
			fail("missing capability %s in %#v", name, raw)
		}
	}
}

func requireProvider(object map[string]any, name string, available bool) {
	provider := findProvider(object, name)
	if got, _ := provider["available"].(bool); got != available {
		fail("provider %s available=%t want=%t provider=%#v", name, got, available, provider)
	}
	state := strings.TrimSpace(fmt.Sprint(provider["state"]))
	if state == "" {
		fail("provider %s missing state: %#v", name, provider)
	}
}

func requireProviderUnavailableDetail(object map[string]any, name string) {
	provider := findProvider(object, name)
	if got, _ := provider["available"].(bool); got {
		fail("provider %s available=true, want unavailable provider=%#v", name, provider)
	}
	if strings.TrimSpace(fmt.Sprint(provider["reason"])) == "" {
		fail("provider %s missing unavailable reason: %#v", name, provider)
	}
	rawDependencies, _ := provider["dependencies"].([]any)
	if len(rawDependencies) == 0 {
		fail("provider %s missing dependency detail: %#v", name, provider)
	}
	for _, item := range rawDependencies {
		dependency, _ := item.(map[string]any)
		if strings.TrimSpace(fmt.Sprint(dependency["name"])) == "" {
			fail("provider %s has dependency without name: %#v", name, item)
		}
	}
}

func requireProviderStatusAgrees(capabilities map[string]any, name string, status map[string]any) {
	provider := findProvider(capabilities, name)
	if providerAvailable, _ := provider["available"].(bool); providerAvailable != status["available"] {
		fail("provider %s availability mismatch provider=%#v status=%#v", name, provider, status)
	}
	if backend := strings.TrimSpace(fmt.Sprint(provider["backend"])); backend != "" && status["backend"] != nil {
		if got := strings.TrimSpace(fmt.Sprint(status["backend"])); got != "" && got != backend {
			fail("provider %s backend mismatch provider=%#v status=%#v", name, provider, status)
		}
	}
}

func findProvider(object map[string]any, name string) map[string]any {
	raw, ok := object["providers"].([]any)
	if !ok {
		fail("missing providers: %#v", object)
	}
	for _, item := range raw {
		provider, _ := item.(map[string]any)
		if fmt.Sprint(provider["name"]) != name {
			continue
		}
		return provider
	}
	fail("missing provider %s in %#v", name, raw)
	return nil
}

func mustBool(object map[string]any, key string, want bool) {
	got, ok := object[key].(bool)
	if !ok || got != want {
		fail("%s=%#v want %t in %#v", key, object[key], want, object)
	}
}

func mustInt(object map[string]any, key string, want int) {
	var got int
	switch raw := object[key].(type) {
	case float64:
		got = int(raw)
	case json.Number:
		value, _ := strconv.Atoi(raw.String())
		got = value
	default:
		fail("%s=%#v want %d in %#v", key, object[key], want, object)
	}
	if got != want {
		fail("%s=%d want %d in %#v", key, got, want, object)
	}
}

func mustString(object map[string]any, key string) string {
	got, ok := object[key].(string)
	if !ok {
		fail("%s=%#v want string in %#v", key, object[key], object)
	}
	return got
}

func mustStringValue(object map[string]any, key string, want string) {
	if got := mustString(object, key); got != want {
		fail("%s=%q want %q in %#v", key, got, want, object)
	}
}

func mustFile(path string, want string) {
	data, err := os.ReadFile(path)
	if err != nil {
		fail("read file %s: %v", path, err)
	}
	if string(data) != want {
		fail("%s=%q want %q", path, string(data), want)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
