package serviceproxy

import (
	"net/http"
	"strings"
)

func cloneUpstreamRequest(r *http.Request) (*http.Request, bool, error) {
	req := r.Clone(r.Context())
	req.URL.Scheme = "http"
	req.URL.Host = "allocation.local"
	req.RequestURI = ""
	sharedBody := req.Body != nil && req.Body != http.NoBody
	if r.Body != nil && r.Body != http.NoBody && r.GetBody != nil {
		body, err := r.GetBody()
		if err != nil {
			return nil, false, err
		}
		req.Body = body
		sharedBody = false
	}
	return req, sharedBody, nil
}

func requestBodyReplayable(r *http.Request) bool {
	return r.Body == nil || r.Body == http.NoBody || r.GetBody != nil
}

func RequestEndpointRetryable(r *http.Request) bool {
	if !requestBodyReplayable(r) {
		return false
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

func upstreamHeaders(header http.Header) http.Header {
	out := header.Clone()
	for _, value := range header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if key := strings.TrimSpace(token); key != "" {
				out.Del(key)
			}
		}
	}
	for _, key := range hopByHopHeaders {
		out.Del(key)
	}
	return out
}

var hopByHopHeaders = []string{
	"Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Proxy-Connection",
	"TE",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}
