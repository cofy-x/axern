package main

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	url := flag.String("url", "", "terminal websocket URL")
	caPath := flag.String("tls-ca-cert", "", "server CA certificate PEM")
	certPath := flag.String("tls-cert", "", "Principal client certificate PEM")
	keyPath := flag.String("tls-key", "", "Principal client private key PEM")
	stdin := flag.String("stdin", "echo gateway-terminal-ok\nexit\n", "stdin to send")
	expect := flag.String("expect", "gateway-terminal-ok", "stdout substring to wait for")
	expectCRLF := flag.String("expect-crlf", "", "stdout substring that must include terminal CRLF line endings")
	timeout := flag.Duration("timeout", 30*time.Second, "smoke timeout")
	flag.Parse()

	if !strings.HasPrefix(*url, "wss://") {
		exitf("-url must use wss://")
	}

	cert, err := tls.LoadX509KeyPair(*certPath, *keyPath)
	if err != nil {
		exitf("load client identity: %v", err)
	}
	ca, err := os.ReadFile(*caPath)
	if err != nil {
		exitf("read server CA: %v", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		exitf("invalid server CA")
	}
	dialer := websocket.Dialer{HandshakeTimeout: *timeout, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{cert}}}
	conn, resp, err := dialer.Dial(*url, nil)
	if err != nil {
		if resp != nil {
			exitf("websocket dial failed: %v status=%s", err, resp.Status)
		}
		exitf("websocket dial failed: %v", err)
	}
	defer conn.Close()

	deadline := time.Now().Add(*timeout)
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	terminalInput := strings.ReplaceAll(*stdin, "\n", "\r")
	if err := conn.WriteJSON(map[string]string{"type": "stdin", "data": terminalInput}); err != nil {
		exitf("write stdin failed: %v", err)
	}

	var stdout strings.Builder
	_ = conn.SetReadDeadline(deadline)
	for time.Now().Before(deadline) {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				break
			}
			exitf("terminal read failed: %v stdout=%q", err, stdout.String())
		}
		if messageType != websocket.TextMessage {
			continue
		}
		var msg struct {
			Type     string `json:"type"`
			Data     string `json:"data"`
			Message  string `json:"message"`
			ExitCode int32  `json:"exit_code"`
		}
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		switch msg.Type {
		case "stdout":
			stdout.WriteString(msg.Data)
			if strings.Contains(stdout.String(), *expect) {
				if *expectCRLF != "" && !strings.Contains(stdout.String(), *expectCRLF) {
					continue
				}
				fmt.Println("gateway_terminal_smoke_ok=true")
				return
			}
		case "stderr":
			stdout.WriteString(msg.Data)
			if strings.Contains(stdout.String(), *expect) {
				if *expectCRLF != "" && !strings.Contains(stdout.String(), *expectCRLF) {
					continue
				}
				fmt.Println("gateway_terminal_smoke_ok=true")
				return
			}
		case "error":
			exitf("terminal error: %s", msg.Message)
		case "exit":
			if strings.Contains(stdout.String(), *expect) && (*expectCRLF == "" || strings.Contains(stdout.String(), *expectCRLF)) {
				fmt.Println("gateway_terminal_smoke_ok=true")
				return
			}
			exitf("terminal exited before expected output; exit_code=%d stdout=%q", msg.ExitCode, stdout.String())
		}
	}
	exitf("terminal smoke timed out waiting for %q; stdout=%q", *expect, stdout.String())
}

func exitf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
	os.Exit(1)
}
