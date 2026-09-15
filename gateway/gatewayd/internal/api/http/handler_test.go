package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandlerServesHealth(t *testing.T) {
	recorder := httptest.NewRecorder()
	New(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
}

func TestHandlerRejectsRemovedServiceRoutes(t *testing.T) {
	recorder := httptest.NewRecorder()
	New(nil).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/svc/default/svc-1/8080", nil))
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
}
