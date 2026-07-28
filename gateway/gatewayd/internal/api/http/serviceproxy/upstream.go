package serviceproxy

import (
	"context"
	"io"
	"net/http"
	"strings"
)

func copyUpstreamResponse(w http.ResponseWriter, resp *http.Response) error {
	defer resp.Body.Close()
	for key, values := range resp.Header {
		if strings.EqualFold(key, GatewayErrorClassHeader) {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, err := io.Copy(w, resp.Body)
	return err
}

type cancelOnCloseReadCloser struct {
	io.ReadCloser
	cancel        context.CancelFunc
	requestBodies []ioCloser
}

func (c cancelOnCloseReadCloser) Close() error {
	err := c.ReadCloser.Close()
	c.cancel()
	if closeErr := closeRequestBodies(c.requestBodies); err == nil {
		err = closeErr
	}
	return err
}
