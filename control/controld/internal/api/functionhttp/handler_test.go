package functionhttp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBundleHandlerRequiresTokenAndReturnsPayload(t *testing.T) {
	handler := New(Config{
		ReadBundle: func(_ context.Context, storageURI string) (BundlePayload, bool, error) {
			if storageURI != "axern://function-bundles/abc.tar" {
				t.Fatalf("storageURI = %q", storageURI)
			}
			return BundlePayload{
				Digest:    "sha256:abc",
				MediaType: "application/vnd.axern.function.tar",
				SizeBytes: 7,
				Payload:   []byte("payload"),
			}, true, nil
		},
		Token: "secret",
	})

	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, BundlePathPrefix+"abc.tar", nil))
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	req := httptest.NewRequest(http.MethodGet, BundlePathPrefix+"abc.tar", nil)
	req.Header.Set("Authorization", "Bearer secret")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if recorder.Body.String() != "payload" {
		t.Fatalf("body = %q", recorder.Body.String())
	}
	if recorder.Header().Get("Content-Type") != "application/vnd.axern.function.tar" {
		t.Fatalf("content type = %q", recorder.Header().Get("Content-Type"))
	}
	if recorder.Header().Get("X-Axern-Function-Bundle-Digest") != "sha256:abc" {
		t.Fatalf("digest header = %q", recorder.Header().Get("X-Axern-Function-Bundle-Digest"))
	}
}
