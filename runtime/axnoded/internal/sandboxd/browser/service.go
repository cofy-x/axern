package browser

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/provider"
)

type Service struct {
	provider     provider.Provider
	command      string
	waiter       *proc.Waiter
	mu           sync.Mutex
	cmd          *exec.Cmd
	running      bool
	url          string
	startedAt    *time.Time
	lastActionAt *time.Time
	lastError    string
}

func NewService(item provider.Provider, waiter *proc.Waiter) *Service {
	command, _ := ResolveCommand()
	return &Service{provider: item, command: command, waiter: waiter}
}

func (s *Service) Provider() provider.Provider {
	return s.provider
}

func (s *Service) Status() StatusResponse {
	if s == nil {
		return StatusResponse{Available: false, Reason: "browser provider unavailable"}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reapLocked()
	resp := StatusResponse{
		Available:    s.provider.Available,
		Command:      s.command,
		Running:      s.running,
		URL:          s.url,
		Reason:       s.provider.Reason,
		StartedAt:    cloneTime(s.startedAt),
		LastActionAt: cloneTime(s.lastActionAt),
		LastError:    s.lastError,
	}
	if s.cmd != nil && s.cmd.Process != nil {
		resp.Pid = s.cmd.Process.Pid
	}
	return resp
}

func (s *Service) Open(ctx context.Context, request OpenRequest) (StatusResponse, error) {
	if err := s.requireAvailable(); err != nil {
		return StatusResponse{}, err
	}
	targetURL := strings.TrimSpace(request.URL)
	if targetURL == "" {
		targetURL = "about:blank"
	}
	if err := validateURL(targetURL); err != nil {
		return StatusResponse{}, err
	}
	if hook := strings.TrimSpace(os.Getenv("AXERN_SANDBOXD_BROWSER_OPEN_CMD")); hook != "" {
		if err := runHook(ctx, s.waiter, hook, targetURL); err != nil {
			s.markError(err)
			return StatusResponse{}, err
		}
		s.mu.Lock()
		s.running = true
		s.url = targetURL
		s.markSessionStartedLocked(targetURL)
		s.mu.Unlock()
		return s.Status(), nil
	}
	s.mu.Lock()
	s.reapLocked()
	if s.cmd != nil && s.cmd.Process != nil {
		status := s.statusLocked()
		s.mu.Unlock()
		if status.URL == targetURL {
			s.mu.Lock()
			s.markActionLocked()
			status = s.statusLocked()
			s.mu.Unlock()
			return status, nil
		}
		return s.Navigate(ctx, NavigateRequest{URL: targetURL})
	}
	cmd := exec.Command(s.command, browserArgs(s.command, targetURL)...)
	cmd.Env = os.Environ()
	cmd.SysProcAttr = proc.SysProcAttr()
	if err := cmd.Start(); err != nil {
		s.mu.Unlock()
		wrapped := fmt.Errorf("%w: start browser: %w", ErrCommandFailed, err)
		s.markError(wrapped)
		return StatusResponse{}, wrapped
	}
	s.cmd = cmd
	s.running = true
	s.url = targetURL
	s.markSessionStartedLocked(targetURL)
	s.watch(cmd)
	status := s.statusLocked()
	s.mu.Unlock()
	return status, nil
}

func (s *Service) Close(ctx context.Context) (StatusResponse, error) {
	if err := s.requireAvailable(); err != nil {
		return StatusResponse{}, err
	}
	if hook := strings.TrimSpace(os.Getenv("AXERN_SANDBOXD_BROWSER_CLOSE_CMD")); hook != "" {
		if err := runHook(ctx, s.waiter, hook, ""); err != nil {
			s.markError(err)
			return StatusResponse{}, err
		}
		s.mu.Lock()
		s.running = false
		s.cmd = nil
		s.url = ""
		s.startedAt = nil
		s.markActionLocked()
		s.mu.Unlock()
		return s.Status(), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cmd != nil && s.cmd.Process != nil {
		_ = proc.KillProcessGroup(s.cmd.Process.Pid)
	}
	s.running = false
	s.cmd = nil
	s.url = ""
	s.startedAt = nil
	s.markActionLocked()
	return s.statusLocked(), nil
}

func (s *Service) requireAvailable() error {
	if s == nil || !s.provider.Available {
		reason := "browser provider unavailable"
		if s != nil && s.provider.Reason != "" {
			reason = s.provider.Reason
		}
		return fmt.Errorf("%w: %s", ErrUnavailable, reason)
	}
	if strings.TrimSpace(s.command) == "" {
		return fmt.Errorf("%w: browser command unavailable", ErrUnavailable)
	}
	return nil
}

func (s *Service) statusLocked() StatusResponse {
	resp := StatusResponse{
		Available:    s.provider.Available,
		Command:      s.command,
		Running:      s.running,
		URL:          s.url,
		Reason:       s.provider.Reason,
		StartedAt:    cloneTime(s.startedAt),
		LastActionAt: cloneTime(s.lastActionAt),
		LastError:    s.lastError,
	}
	if s.cmd != nil && s.cmd.Process != nil {
		resp.Pid = s.cmd.Process.Pid
	}
	return resp
}

func (s *Service) markSessionStartedLocked(targetURL string) {
	now := time.Now().UTC()
	s.startedAt = &now
	s.lastActionAt = &now
	s.lastError = ""
	s.url = targetURL
}

func (s *Service) markActionLocked() {
	now := time.Now().UTC()
	s.lastActionAt = &now
	s.lastError = ""
}

func (s *Service) markError(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markActionLocked()
	s.lastError = err.Error()
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (s *Service) watch(cmd *exec.Cmd) {
	go func() {
		if s.waiter != nil {
			<-s.waiter.Watch(cmd)
		} else {
			_ = cmd.Wait()
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.cmd == cmd {
			s.cmd = nil
			s.running = false
			s.url = ""
			s.startedAt = nil
			s.markActionLocked()
		}
	}()
}

func (s *Service) reapLocked() {
	if s.cmd == nil || s.cmd.Process == nil {
		return
	}
	if err := s.cmd.Process.Signal(syscall.Signal(0)); err != nil {
		s.cmd = nil
		s.running = false
		s.url = ""
		s.startedAt = nil
		s.lastError = err.Error()
	}
}

func browserArgs(command string, targetURL string) []string {
	switch filepath.Base(command) {
	case "firefox":
		return []string{"-new-window", targetURL}
	default:
		return []string{
			"--no-sandbox",
			"--disable-dev-shm-usage",
			"--disable-gpu",
			"--user-data-dir=/tmp/axern-browser-profile",
			targetURL,
		}
	}
}

func validateURL(value string) error {
	if value == "about:blank" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return fmt.Errorf("%w: invalid browser URL", ErrInvalidArgument)
	}
	switch parsed.Scheme {
	case "http", "https", "file", "about", "data":
		return nil
	default:
		return fmt.Errorf("%w: unsupported browser URL scheme %q", ErrInvalidArgument, parsed.Scheme)
	}
}
