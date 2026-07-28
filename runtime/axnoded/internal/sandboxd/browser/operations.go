package browser

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
)

func (s *Service) Navigate(ctx context.Context, request NavigateRequest) (StatusResponse, error) {
	if err := s.requireAvailable(); err != nil {
		return StatusResponse{}, err
	}
	targetURL := strings.TrimSpace(request.URL)
	if targetURL == "" {
		return StatusResponse{}, fmt.Errorf("%w: browser URL is required", ErrInvalidArgument)
	}
	if err := validateURL(targetURL); err != nil {
		return StatusResponse{}, err
	}
	if strings.TrimSpace(os.Getenv("AXERN_SANDBOXD_BROWSER_OPEN_CMD")) != "" {
		return s.Open(ctx, OpenRequest{URL: targetURL})
	}
	s.mu.Lock()
	s.reapLocked()
	running := s.running
	s.mu.Unlock()
	if !running {
		return s.Open(ctx, OpenRequest{URL: targetURL})
	}
	if _, err := proc.RunCommandOutput(ctx, s.waiter, s.command, browserArgs(s.command, targetURL), nil, defaultHookWait); err != nil {
		wrapped := fmt.Errorf("%w: navigate browser: %w", ErrCommandFailed, err)
		s.markError(wrapped)
		return StatusResponse{}, wrapped
	}
	s.mu.Lock()
	s.url = targetURL
	s.markActionLocked()
	s.mu.Unlock()
	return s.Status(), nil
}

func (s *Service) Resize(ctx context.Context, request ResizeRequest) (StatusResponse, error) {
	if err := s.requireRunning(); err != nil {
		return StatusResponse{}, err
	}
	if request.Width <= 0 || request.Height <= 0 {
		return StatusResponse{}, fmt.Errorf("%w: browser width and height must be positive", ErrInvalidArgument)
	}
	if request.Width > 10000 || request.Height > 10000 {
		return StatusResponse{}, fmt.Errorf("%w: browser width and height are too large", ErrInvalidArgument)
	}
	if _, err := proc.RunCommandOutput(ctx, s.waiter, "xdotool", []string{
		"getactivewindow",
		"windowsize",
		strconv.Itoa(request.Width),
		strconv.Itoa(request.Height),
	}, nil, defaultHookWait); err != nil {
		wrapped := fmt.Errorf("%w: resize browser window: %w", ErrCommandFailed, err)
		s.markError(wrapped)
		return StatusResponse{}, wrapped
	}
	s.mu.Lock()
	s.markActionLocked()
	s.mu.Unlock()
	return s.Status(), nil
}

func (s *Service) Click(ctx context.Context, request ClickRequest) (StatusResponse, error) {
	if err := s.requireRunning(); err != nil {
		return StatusResponse{}, err
	}
	if request.X < 0 || request.Y < 0 {
		return StatusResponse{}, fmt.Errorf("%w: browser click coordinates must be non-negative", ErrInvalidArgument)
	}
	button := strings.TrimSpace(request.Button)
	if button == "" {
		button = "1"
	}
	if _, err := proc.RunCommandOutput(ctx, s.waiter, "xdotool", []string{
		"mousemove",
		strconv.Itoa(request.X),
		strconv.Itoa(request.Y),
		"click",
		button,
	}, nil, defaultHookWait); err != nil {
		wrapped := fmt.Errorf("%w: click browser window: %w", ErrCommandFailed, err)
		s.markError(wrapped)
		return StatusResponse{}, wrapped
	}
	s.mu.Lock()
	s.markActionLocked()
	s.mu.Unlock()
	return s.Status(), nil
}

func (s *Service) Type(ctx context.Context, request TypeRequest) (StatusResponse, error) {
	if err := s.requireRunning(); err != nil {
		return StatusResponse{}, err
	}
	if request.Text == "" {
		return StatusResponse{}, fmt.Errorf("%w: browser type text is required", ErrInvalidArgument)
	}
	if request.DelayMS < 0 {
		return StatusResponse{}, fmt.Errorf("%w: browser type delay must be non-negative", ErrInvalidArgument)
	}
	args := []string{"type"}
	if request.DelayMS > 0 {
		args = append(args, "--delay", strconv.Itoa(request.DelayMS))
	}
	args = append(args, "--", request.Text)
	if _, err := proc.RunCommandOutput(ctx, s.waiter, "xdotool", args, nil, defaultHookWait); err != nil {
		wrapped := fmt.Errorf("%w: type into browser window: %w", ErrCommandFailed, err)
		s.markError(wrapped)
		return StatusResponse{}, wrapped
	}
	s.mu.Lock()
	s.markActionLocked()
	s.mu.Unlock()
	return s.Status(), nil
}

func (s *Service) Wait(ctx context.Context, request WaitRequest) (StatusResponse, error) {
	if err := s.requireRunning(); err != nil {
		return StatusResponse{}, err
	}
	timeout := request.TimeoutMS
	if timeout <= 0 {
		timeout = 250
	}
	if timeout > 30000 {
		return StatusResponse{}, fmt.Errorf("%w: browser wait timeout is too large", ErrInvalidArgument)
	}
	timer := time.NewTimer(time.Duration(timeout) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		wrapped := fmt.Errorf("%w: %w", ErrCommandFailed, ctx.Err())
		s.markError(wrapped)
		return StatusResponse{}, wrapped
	case <-timer.C:
		s.mu.Lock()
		s.markActionLocked()
		s.mu.Unlock()
		return s.Status(), nil
	}
}

func (s *Service) requireRunning() error {
	if err := s.requireAvailable(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reapLocked()
	if !s.running {
		return fmt.Errorf("%w: browser is not running", ErrUnavailable)
	}
	return nil
}
