package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

func TestHTTPPreviewHasFixedLoopbackAndBoundedResponses(t *testing.T) {
	guest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oversized":
			_, _ = io.CopyN(w, strings.NewReader(strings.Repeat("x", wire.MaxHTTPPreviewBodyBytes+1)), wire.MaxHTTPPreviewBodyBytes+1)
		case "/redirect":
			http.Redirect(w, r, "http://other.invalid/secret", http.StatusFound)
		default:
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Set-Cookie", "untrusted=1")
			_, _ = io.WriteString(w, r.Method+" "+r.URL.RequestURI())
		}
	}))
	t.Cleanup(guest.Close)
	_, portText, err := net.SplitHostPort(guest.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.ParseUint(portText, 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, method, target string
		status               int
	}{
		{"local", http.MethodGet, "/page?q=42", http.StatusOK},
		{"head", http.MethodHead, "/page", http.StatusOK},
		{"absolute", http.MethodGet, "http://other.invalid/", http.StatusBadRequest},
		{"relative authority", http.MethodGet, "//other.invalid/", http.StatusBadRequest},
		{"encoded authority", http.MethodGet, "/%2fother.invalid/", http.StatusBadRequest},
		{"encoded backslash", http.MethodGet, "/%5cother.invalid/", http.StatusBadRequest},
		{"connect", http.MethodConnect, "/", http.StatusBadRequest},
		{"redirect", http.MethodGet, "/redirect", http.StatusPreconditionFailed},
		{"oversized", http.MethodGet, "/oversized", http.StatusPreconditionFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			input, err := json.Marshal(wire.HTTPPreviewRequest{Port: uint32(port), Method: tc.method, Target: tc.target})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, wire.PathHTTPPreview, bytes.NewReader(input))
			response := httptest.NewRecorder()
			(&Server{}).handleHTTPPreview(response, request)
			if response.Code != tc.status {
				t.Fatalf("preview returned HTTP %d, want %d", response.Code, tc.status)
			}
			if tc.name == "local" {
				var result wire.HTTPPreviewResponse
				if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if got := string(result.Body); got != "GET /page?q=42" || result.ContentType != "text/plain" {
					t.Fatalf("guest response lost: %+v", result)
				}
				if strings.Contains(response.Body.String(), "untrusted") {
					t.Fatal("guest cookie crossed the preview boundary")
				}
			}
		})
	}
}
