package computeruse

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/proc"
	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/provider"
)

var (
	ErrUnavailable     = errors.New("computer_use provider unavailable")
	ErrCommandFailed   = errors.New("computer_use command failed")
	ErrInvalidArgument = errors.New("computer_use invalid argument")
)

const statusProbeTimeout = 750 * time.Millisecond

type Service struct {
	provider provider.Provider
	waiter   *proc.Waiter
	display  string
	backend  string
}

func NewService(item provider.Provider, waiter *proc.Waiter) *Service {
	display := strings.TrimSpace(os.Getenv("DISPLAY"))
	return &Service{
		provider: item,
		waiter:   waiter,
		display:  display,
		backend:  "x11",
	}
}

func (s *Service) Provider() provider.Provider {
	return s.provider
}

func (s *Service) Status(ctx context.Context) StatusResponse {
	deps := s.dependencies(ctx)
	response := StatusResponse{
		Available: s.provider.Available,
		Display:   s.display,
		Backend:   s.backend,
		Reason:    s.provider.Reason,
	}
	if response.Available {
		response.Available, response.Reason = summarizeDependencies(deps)
	}
	response.Dependencies = deps
	return response
}

func (s *Service) requireAvailable() error {
	if s == nil {
		return ErrUnavailable
	}
	if !s.provider.Available {
		return fmt.Errorf("%w: %s", ErrUnavailable, s.provider.Reason)
	}
	return nil
}

func (s *Service) dependencies(ctx context.Context) []DependencyStatus {
	deps := []DependencyStatus{
		dependency("display_env", strings.TrimSpace(s.display) != "", "DISPLAY is not set"),
		screenshotBackendDependency(),
		displayBackendDependency(),
		inputBackendDependency(),
	}
	deps = append(deps, s.displayServerDependency(ctx))
	return deps
}

func (s *Service) displayServerDependency(ctx context.Context) DependencyStatus {
	if err := s.requireAvailable(); err != nil {
		return DependencyStatus{Name: "display_server", Available: false, Reason: err.Error()}
	}
	probeCtx, cancel := context.WithTimeout(ctx, statusProbeTimeout)
	defer cancel()
	if _, err := s.Display(probeCtx); err != nil {
		return DependencyStatus{Name: "display_server", Available: false, Reason: err.Error()}
	}
	return DependencyStatus{Name: "display_server", Available: true}
}

func dependency(name string, available bool, reason string) DependencyStatus {
	if available {
		return DependencyStatus{Name: name, Available: true}
	}
	return DependencyStatus{Name: name, Available: false, Reason: reason}
}

func summarizeDependencies(items []DependencyStatus) (bool, string) {
	for _, item := range items {
		if !item.Available {
			if item.Reason != "" {
				return false, item.Name + " unavailable: " + item.Reason
			}
			return false, item.Name + " unavailable"
		}
	}
	return true, ""
}
