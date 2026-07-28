package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

type responderProcess struct {
	cmd *exec.Cmd
}

func startResponder(namespace, mode, listenAddress string) (*responderProcess, error) {
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve verify-egress binary: %w", err)
	}

	cmd := exec.Command("ip", "netns", "exec", namespace, self, "-mode", mode, "-listen-address", listenAddress)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open responder stdout: %w", err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", mode, err)
	}

	reader := bufio.NewReader(stdout)
	ready, err := reader.ReadString('\n')
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("wait for %s readiness: %w", mode, err)
	}
	if strings.TrimSpace(ready) != "responder_ready=true" {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("unexpected %s readiness marker %q", mode, strings.TrimSpace(ready))
	}
	go io.Copy(io.Discard, reader)

	return &responderProcess{cmd: cmd}, nil
}

func (p *responderProcess) stop() {
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = p.cmd.Process.Kill()
	_ = p.cmd.Wait()
}

func runTCPResponder(listenAddress string) error {
	if listenAddress == "" {
		return fmt.Errorf("listen-address is required")
	}
	listener, err := net.Listen("tcp4", listenAddress)
	if err != nil {
		return err
	}
	defer listener.Close()
	fmt.Println("responder_ready=true")

	for {
		conn, err := listener.Accept()
		if err != nil {
			return err
		}
		go serveTCPResponderConn(conn)
	}
}

func serveTCPResponderConn(conn net.Conn) {
	defer conn.Close()
	remote, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return
	}
	payload := fmt.Sprintf("source=%s\n", remote.IP.String())
	if _, err := io.WriteString(conn, payload); err != nil {
		fmt.Fprintf(os.Stderr, "tcp_responder_write_error=%v\n", err)
		return
	}

	reader := bufio.NewReader(conn)
	for {
		if _, err := reader.ReadString('\n'); err != nil {
			if err != io.EOF {
				fmt.Fprintf(os.Stderr, "tcp_responder_read_error=%v\n", err)
			}
			return
		}
		if _, err := io.WriteString(conn, payload); err != nil {
			fmt.Fprintf(os.Stderr, "tcp_responder_write_error=%v\n", err)
			return
		}
	}
}

func runUDPResponder(listenAddress string) error {
	if listenAddress == "" {
		return fmt.Errorf("listen-address is required")
	}
	addr, err := net.ResolveUDPAddr("udp4", listenAddress)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	fmt.Println("responder_ready=true")

	buf := make([]byte, 512)
	for {
		_, remote, err := conn.ReadFromUDP(buf)
		if err != nil {
			return err
		}
		if _, err := conn.WriteToUDP([]byte(fmt.Sprintf("source=%s\n", remote.IP.String())), remote); err != nil {
			return err
		}
	}
}

func runICMPResponder(listenAddress string) error {
	if listenAddress == "" {
		return fmt.Errorf("listen-address is required")
	}

	conn, err := icmp.ListenPacket("ip4:icmp", listenAddress)
	if err != nil {
		return err
	}
	defer conn.Close()
	fmt.Println("responder_ready=true")

	buf := make([]byte, 1500)
	for {
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			return err
		}
		ipAddr, ok := peer.(*net.IPAddr)
		if !ok {
			continue
		}
		msg, err := icmp.ParseMessage(ipv4.ICMPTypeEcho.Protocol(), buf[:n])
		if err != nil {
			continue
		}
		if msg.Type != ipv4.ICMPTypeEcho {
			continue
		}
		echo, ok := msg.Body.(*icmp.Echo)
		if !ok {
			continue
		}
		fmt.Fprintf(os.Stderr, "icmp_request_source=%s\n", ipAddr.IP.String())
		reply := icmp.Message{
			Type: ipv4.ICMPTypeEchoReply,
			Code: 0,
			Body: &icmp.Echo{
				ID:   echo.ID,
				Seq:  echo.Seq,
				Data: echo.Data,
			},
		}
		wire, err := reply.Marshal(nil)
		if err != nil {
			return err
		}
		if _, err := conn.WriteTo(wire, ipAddr); err != nil {
			return err
		}
	}
}
