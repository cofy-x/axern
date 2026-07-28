package server

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeOptionalJSONRequestTreatsWhitespaceAsEmpty(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(" \n\t "))
	var target struct {
		Name string `json:"name"`
	}
	if err := decodeOptionalJSONRequest(req, &target); err != nil {
		t.Fatalf("decodeOptionalJSONRequest() error = %v", err)
	}
	if target.Name != "" {
		t.Fatalf("target = %#v, want zero value", target)
	}
}

func TestDecodeRequiredJSONRequestRejectsWhitespaceOnly(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(" \n\t "))
	var target struct {
		Name string `json:"name"`
	}
	if err := decodeRequiredJSONRequest(req, &target); !errors.Is(err, io.EOF) {
		t.Fatalf("decodeRequiredJSONRequest() error = %v, want EOF", err)
	}
}

func TestErrorCodeForStatusUsesProviderTaxonomy(t *testing.T) {
	tests := []struct {
		status int
		want   string
	}{
		{status: http.StatusBadRequest, want: errorCodeInvalidArgument},
		{status: http.StatusConflict, want: errorCodeAlreadyExists},
		{status: http.StatusNotFound, want: errorCodeNotFound},
		{status: http.StatusPreconditionFailed, want: errorCodeFailedCondition},
		{status: http.StatusRequestTimeout, want: errorCodeTimeout},
		{status: http.StatusServiceUnavailable, want: errorCodeUnavailable},
	}
	for _, tt := range tests {
		if got := errorCodeForStatus(tt.status); got != tt.want {
			t.Fatalf("errorCodeForStatus(%d) = %q, want %q", tt.status, got, tt.want)
		}
	}
}
