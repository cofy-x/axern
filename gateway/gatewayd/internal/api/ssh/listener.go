package sshapi

import (
	"context"
	"errors"
	"net"

	"github.com/sirupsen/logrus"
	gossh "golang.org/x/crypto/ssh"
)

func (s *Server) Run(ctx context.Context) error {
	if s == nil {
		return nil
	}
	ln, err := net.Listen("tcp", s.address)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	logrus.WithField("address", ln.Addr().String()).Info("gateway ssh listener started")

	errCh := make(chan error, 1)
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	go s.acceptLoop(ctx, ln, errCh)
	err = <-errCh
	s.wg.Wait()
	return err
}

func (s *Server) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Close()
	}
	return nil
}

func (s *Server) acceptLoop(ctx context.Context, ln net.Listener, errCh chan<- error) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				errCh <- nil
				return
			}
			errCh <- err
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handleConn(ctx, conn)
		}()
	}
}

func (s *Server) handleConn(parent context.Context, raw net.Conn) {
	defer raw.Close()
	conn, chans, reqs, err := gossh.NewServerConn(raw, s.config)
	if err != nil {
		logrus.WithError(err).Debug("ssh handshake failed")
		return
	}
	defer conn.Close()
	go gossh.DiscardRequests(reqs)

	connCtx, cancel := context.WithCancel(parent)
	defer cancel()
	go func() {
		<-parent.Done()
		_ = conn.Close()
	}()

	for ch := range chans {
		if ch.ChannelType() != "session" {
			_ = ch.Reject(gossh.UnknownChannelType, "only session channels are supported")
			continue
		}
		channel, requests, err := ch.Accept()
		if err != nil {
			continue
		}
		s.wg.Add(1)
		go func(allocationID string) {
			defer s.wg.Done()
			s.handleSession(connCtx, allocationID, channel, requests)
		}(conn.User())
	}
}
