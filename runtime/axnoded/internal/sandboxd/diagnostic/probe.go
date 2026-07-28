package diagnostic

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type ProbeRequest struct {
	Host      string     `json:"host,omitempty"`
	TimeoutMS int        `json:"timeoutMs,omitempty"`
	HTTP      *HTTPProbe `json:"http,omitempty"`
	TCP       *TCPProbe  `json:"tcp,omitempty"`
}

type HTTPProbe struct {
	Port   int    `json:"port,omitempty"`
	Path   string `json:"path,omitempty"`
	Scheme string `json:"scheme,omitempty"`
}

type TCPProbe struct {
	Port int `json:"port,omitempty"`
}

type ProbeResponse struct {
	OK       bool   `json:"ok"`
	Kind     string `json:"kind"`
	Target   string `json:"target"`
	Detail   string `json:"detail,omitempty"`
	Duration int64  `json:"durationMs"`
}

func Probe(request ProbeRequest) ProbeResponse {
	start := time.Now()
	timeout := time.Duration(request.TimeoutMS) * time.Millisecond
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	host := strings.TrimSpace(request.Host)
	if host == "" {
		host = "127.0.0.1"
	}
	switch {
	case request.HTTP != nil:
		ok, target, detail := probeHTTP(host, *request.HTTP, timeout)
		return probeResult(ok, "http", target, detail, start)
	case request.TCP != nil:
		ok, target, detail := probeTCP(host, *request.TCP, timeout)
		return probeResult(ok, "tcp", target, detail, start)
	default:
		return probeResult(false, "", "", "probe target is required", start)
	}
}

func probeHTTP(host string, probe HTTPProbe, timeout time.Duration) (bool, string, string) {
	if probe.Port <= 0 {
		return false, "", "http probe port must be positive"
	}
	scheme := strings.ToLower(strings.TrimSpace(probe.Scheme))
	if scheme == "" {
		scheme = "http"
	}
	path := strings.TrimSpace(probe.Path)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	target := fmt.Sprintf("%s://%s:%d%s", scheme, host, probe.Port, path)
	transport := &http.Transport{
		DialContext: (&net.Dialer{Timeout: timeout}).DialContext,
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true,
		},
	}
	client := &http.Client{Timeout: timeout, Transport: transport}
	resp, err := client.Get(target)
	if err != nil {
		return false, target, err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		return true, target, ""
	}
	return false, target, fmt.Sprintf("http probe returned %d", resp.StatusCode)
}

func probeTCP(host string, probe TCPProbe, timeout time.Duration) (bool, string, string) {
	if probe.Port <= 0 {
		return false, "", "tcp probe port must be positive"
	}
	target := net.JoinHostPort(host, fmt.Sprintf("%d", probe.Port))
	conn, err := net.DialTimeout("tcp", target, timeout)
	if err != nil {
		return false, target, err.Error()
	}
	_ = conn.Close()
	return true, target, ""
}

func probeResult(ok bool, kind string, target string, detail string, start time.Time) ProbeResponse {
	return ProbeResponse{OK: ok, Kind: kind, Target: target, Detail: detail, Duration: time.Since(start).Milliseconds()}
}
