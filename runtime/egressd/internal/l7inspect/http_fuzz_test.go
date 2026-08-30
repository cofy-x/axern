package l7inspect

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cofy-x/axern/lib/go/networkpolicy"
)

func FuzzReadHTTPRequest(f *testing.F) {
	for _, seed := range [][]byte{
		[]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"),
		[]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"),
		[]byte("GET http://example.com/ HTTP/1.1\r\nHost: other.example\r\n\r\n"),
		[]byte("GET / HTTP/1.1\r\nHost: a.example\r\nHost: b.example\r\n\r\n"),
	} {
		f.Add(seed, uint16(4096))
	}
	f.Fuzz(func(t *testing.T, wire []byte, bound uint16) {
		maxBytes := int(bound)
		if maxBytes == 0 {
			maxBytes = 1
		}
		request, err := ReadHTTPRequest(bytes.NewReader(wire), maxBytes)
		if err != nil {
			return
		}
		if len(request.Bytes) == 0 || len(request.Bytes) > maxBytes || !bytes.HasSuffix(request.Bytes, []byte("\r\n\r\n")) {
			t.Fatalf("successful header escaped its bound: bytes=%d max=%d", len(request.Bytes), maxBytes)
		}
		if strings.EqualFold(request.Method, "CONNECT") || request.Host == "" {
			t.Fatalf("unsupported successful request: %#v", request)
		}
		if !request.DirectIP {
			normalized, err := networkpolicy.NormalizeDomain(request.Host)
			if err != nil || normalized != request.Host {
				t.Fatalf("successful Host is not canonical: host=%q normalized=%q err=%v", request.Host, normalized, err)
			}
		}
	})
}
