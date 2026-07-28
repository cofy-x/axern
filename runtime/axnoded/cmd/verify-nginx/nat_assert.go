package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cofy-x/axern/network/bpfnet"
	"golang.org/x/sys/unix"
)

func assertIptablesRuleAbsentAll(table, chain string, needles ...string) error {
	output, err := exec.Command("iptables", "-t", table, "-S", chain).CombinedOutput()
	if err != nil {
		return fmt.Errorf("iptables -t %s -S %s failed: %v: %s", table, chain, err, strings.TrimSpace(string(output)))
	}
	for _, line := range strings.Split(string(output), "\n") {
		matched := true
		for _, needle := range needles {
			if !strings.Contains(line, needle) {
				matched = false
				break
			}
		}
		if matched {
			return fmt.Errorf("iptables %s/%s unexpectedly contained localhost hairpin rule: %s", table, chain, strings.TrimSpace(line))
		}
	}
	return nil
}

func assertReachable(listenPort int) error {
	url := fmt.Sprintf("http://127.0.0.1:%d/", listenPort)
	client := &http.Client{Timeout: 5 * time.Second}
	var lastErr error
	for attempt := 0; attempt < 15; attempt++ {
		resp, err := client.Get(url)
		if err == nil {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
			lastErr = fmt.Errorf("unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		} else {
			lastErr = err
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("http GET %s failed after retries: %v", url, lastErr)
}

func assertReachableFromNamespace(namespace, host string, listenPort int) error {
	url := fmt.Sprintf("http://%s:%d/", host, listenPort)
	var lastErr error
	for attempt := 0; attempt < 15; attempt++ {
		cmd := exec.Command("ip", "netns", "exec", namespace, "curl", "-fsS", "--max-time", "5", url)
		if output, err := cmd.CombinedOutput(); err == nil {
			return nil
		} else {
			lastErr = fmt.Errorf("%v: %s", err, strings.TrimSpace(string(output)))
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("external GET %s from netns %s failed after retries: %v", url, namespace, lastErr)
}

func assertGetpeernameAlias(listenPort int) error {
	address := fmt.Sprintf("127.0.0.1:%d", listenPort)
	conn, err := net.DialTimeout("tcp4", address, 5*time.Second)
	if err != nil {
		return fmt.Errorf("tcp dial %s failed: %w", address, err)
	}
	defer conn.Close()

	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		return fmt.Errorf("expected *net.TCPConn for %s", address)
	}

	rawConn, err := tcpConn.SyscallConn()
	if err != nil {
		return fmt.Errorf("obtain raw conn for %s: %w", address, err)
	}

	var (
		peerErr error
		peer    unix.Sockaddr
	)
	if err := rawConn.Control(func(fd uintptr) {
		peer, peerErr = unix.Getpeername(int(fd))
	}); err != nil {
		return fmt.Errorf("control raw conn for %s: %w", address, err)
	}
	if peerErr != nil {
		return fmt.Errorf("getpeername for %s failed: %w", address, peerErr)
	}

	sa, ok := peer.(*unix.SockaddrInet4)
	if !ok {
		return fmt.Errorf("expected ipv4 peer for %s, got %T", address, peer)
	}
	if got := net.IP(sa.Addr[:]).String(); got != "127.0.0.1" {
		return fmt.Errorf("unexpected peer address for %s: %s", address, got)
	}
	if sa.Port != listenPort {
		return fmt.Errorf("unexpected peer port for %s: %d", address, sa.Port)
	}
	return nil
}

func assertPinnedLocalhostLinks(pinPath string) error {
	required := []string{
		filepath.Join(pinPath, "links", "localhost-connect4"),
		filepath.Join(pinPath, "links", "localhost-getpeer4"),
		filepath.Join(pinPath, "links", "localhost-release"),
	}
	for _, path := range required {
		if _, err := os.Stat(path); err != nil {
			return fmt.Errorf("expected pinned localhost link %s: %w", path, err)
		}
	}
	return nil
}

func assertLocalhostStats(status bpfnet.Status) error {
	if status.Kernel.LocalhostConnectHits == 0 {
		return fmt.Errorf("expected non-zero localhost connect hits in stats map")
	}
	if status.Kernel.LocalhostGetpeerHits == 0 {
		return fmt.Errorf("expected non-zero localhost getpeer hits in stats map")
	}

	fmt.Printf("bpfnet_localhost_connect_hits=%d\n", status.Kernel.LocalhostConnectHits)
	fmt.Printf("bpfnet_localhost_getpeer_hits=%d\n", status.Kernel.LocalhostGetpeerHits)
	return nil
}
