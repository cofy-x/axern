package proxy

import (
	"bytes"
	"io"
	"net/http"
)

// ReadAndRestoreBody reads the full request body and restores it so the
// request can be forwarded to the upstream server unchanged.
func ReadAndRestoreBody(req *http.Request) ([]byte, error) {
	if req.Body == nil || req.Body == http.NoBody {
		return nil, nil
	}
	body, err := io.ReadAll(req.Body)
	if closeErr := req.Body.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	req.Body = io.NopCloser(bytes.NewReader(body))
	req.ContentLength = int64(len(body))
	return body, err
}
