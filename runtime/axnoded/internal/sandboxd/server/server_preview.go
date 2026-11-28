package server

import (
	"io"
	"net/http"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

var guestPreviewClient = &http.Client{
	Timeout: 8 * time.Second,
	Transport: &http.Transport{
		DisableKeepAlives:      true,
		MaxResponseHeaderBytes: 16 << 10,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

func (s *Server) handleHTTPPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeMethodNotAllowed(w)
		return
	}
	var preview wire.HTTPPreviewRequest
	if err := decodeRequiredJSONRequest(r, &preview); err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, "invalid HTTP preview request")
		return
	}
	destination, err := preview.Destination()
	if err != nil {
		writeError(w, http.StatusBadRequest, errorCodeInvalidArgument, err.Error())
		return
	}
	request := &http.Request{Method: preview.Method, URL: destination, Header: make(http.Header)}
	response, err := guestPreviewClient.Do(request.WithContext(r.Context()))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeUnavailable, "guest HTTP service did not respond")
		return
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode > 599 {
		writeError(w, http.StatusPreconditionFailed, errorCodeFailedCondition, "guest HTTP status is unsupported")
		return
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, wire.MaxHTTPPreviewBodyBytes+1))
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, errorCodeUnavailable, "guest HTTP response could not be read")
		return
	}
	if len(body) > wire.MaxHTTPPreviewBodyBytes {
		writeError(w, http.StatusPreconditionFailed, errorCodeFailedCondition, "guest HTTP response exceeds the preview limit")
		return
	}
	location := response.Header.Get("Location")
	if location != "" {
		// Do not provide a browser with redirects outside this preview origin.
		if _, err := wire.ParseHTTPPreviewTarget(location); err != nil {
			writeError(w, http.StatusPreconditionFailed, errorCodeFailedCondition, "guest HTTP redirect is outside the preview")
			return
		}
	}
	writeJSON(w, http.StatusOK, wire.HTTPPreviewResponse{
		Status: response.StatusCode, ContentType: response.Header.Get("Content-Type"), Body: body, Location: location,
	})
}
