package wire

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

const PathHTTPPreview = "/preview/http"
const MaxHTTPPreviewBodyBytes = 2 << 20

type HTTPPreviewRequest struct {
	Port   uint32 `json:"port"`
	Method string `json:"method"`
	Target string `json:"target"`
}

type HTTPPreviewResponse struct {
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	Body        []byte `json:"body,omitempty"`
	Location    string `json:"location,omitempty"`
}

func (r HTTPPreviewRequest) Destination() (*url.URL, error) {
	if r.Port == 0 || r.Port > 65535 {
		return nil, fmt.Errorf("preview requires a guest port from 1 to 65535")
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return nil, fmt.Errorf("preview supports GET and HEAD")
	}
	target, err := ParseHTTPPreviewTarget(r.Target)
	if err != nil {
		return nil, err
	}
	target.Scheme = "http"
	target.Host = net.JoinHostPort("127.0.0.1", strconv.FormatUint(uint64(r.Port), 10))
	return target, nil
}

func ParseHTTPPreviewTarget(raw string) (*url.URL, error) {
	errInvalid := fmt.Errorf("preview requires a local absolute request path")
	if len(raw) > 4096 || !strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "//") || strings.ContainsAny(raw, "\\#") {
		return nil, errInvalid
	}
	target, err := url.ParseRequestURI(raw)
	if err != nil || target.Host != "" || target.IsAbs() || strings.HasPrefix(target.Path, "//") || strings.ContainsRune(target.Path, '\\') {
		return nil, errInvalid
	}
	return target, nil
}
