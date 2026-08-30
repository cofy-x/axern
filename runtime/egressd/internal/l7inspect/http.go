package l7inspect

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"

	"github.com/cofy-x/axern/lib/go/networkpolicy"
)

const DefaultMaxHTTPHeaderBytes = 32 * 1024

type HTTPRequest struct {
	Method   string
	Host     string
	DirectIP bool
	Bytes    []byte
}

// ReadHTTPRequest consumes exactly one bounded HTTP/1 request header. It does
// not read or retain an application body.
func ReadHTTPRequest(reader io.Reader, maxBytes int) (HTTPRequest, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultMaxHTTPHeaderBytes
	}
	header, err := readHeader(reader, maxBytes)
	if err != nil {
		return HTTPRequest{}, err
	}
	request, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(header)))
	if err != nil {
		return HTTPRequest{}, fmt.Errorf("parse HTTP request header: %w", err)
	}
	if request.URL == nil || request.URL.IsAbs() || request.URL.Host != "" {
		return HTTPRequest{}, fmt.Errorf("absolute-form HTTP requests are not allowed by domain policy")
	}
	if strings.EqualFold(request.Method, http.MethodConnect) {
		return HTTPRequest{}, fmt.Errorf("HTTP CONNECT is not allowed by domain policy")
	}
	host, err := hostWithoutPort(request.Host)
	if err != nil {
		return HTTPRequest{}, err
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return HTTPRequest{Method: request.Method, Host: addr.Unmap().String(), DirectIP: true, Bytes: header}, nil
	}
	host, err = networkpolicy.NormalizeDomain(host)
	if err != nil || strings.HasPrefix(host, "*.") {
		return HTTPRequest{}, fmt.Errorf("invalid HTTP Host")
	}
	return HTTPRequest{Method: request.Method, Host: host, Bytes: header}, nil
}

func readHeader(reader io.Reader, maxBytes int) ([]byte, error) {
	if reader == nil {
		return nil, fmt.Errorf("HTTP request reader is required")
	}
	buffer := make([]byte, 0, min(maxBytes, 4096))
	one := make([]byte, 1)
	for len(buffer) < maxBytes {
		if _, err := io.ReadFull(reader, one); err != nil {
			return nil, fmt.Errorf("read HTTP request header: %w", err)
		}
		buffer = append(buffer, one[0])
		if len(buffer) >= 4 && bytes.Equal(buffer[len(buffer)-4:], []byte("\r\n\r\n")) {
			return buffer, nil
		}
	}
	return nil, fmt.Errorf("HTTP request header exceeds %d bytes", maxBytes)
}

func hostWithoutPort(authority string) (string, error) {
	authority = strings.TrimSpace(authority)
	if authority == "" {
		return "", fmt.Errorf("HTTP Host is required")
	}
	if strings.HasPrefix(authority, "[") {
		host, port, err := net.SplitHostPort(authority)
		if err == nil {
			if err := validatePort(port); err != nil {
				return "", err
			}
			return host, nil
		}
		if strings.HasSuffix(authority, "]") {
			return strings.TrimSuffix(strings.TrimPrefix(authority, "["), "]"), nil
		}
		return "", fmt.Errorf("invalid HTTP Host")
	}
	if strings.Count(authority, ":") == 1 {
		host, port, err := net.SplitHostPort(authority)
		if err != nil || port == "" {
			return "", fmt.Errorf("invalid HTTP Host")
		}
		if err := validatePort(port); err != nil {
			return "", err
		}
		return host, nil
	}
	return authority, nil
}

func validatePort(value string) error {
	port, err := strconv.ParseUint(value, 10, 16)
	if err != nil || port == 0 {
		return fmt.Errorf("invalid HTTP Host port")
	}
	if port != 80 {
		return fmt.Errorf("HTTP Host port must match intercepted TCP port 80")
	}
	return nil
}
