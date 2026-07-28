//go:build !linux

package dataplane

import (
	"fmt"
	"runtime"
)

type stubDataplane struct{}

func New(Config) Interface {
	return &stubDataplane{}
}

func (*stubDataplane) EnsureAttached([]string, string, []string, []Service) (Attachment, error) {
	return Attachment{}, fmt.Errorf("tc dataplane is only supported on linux (current GOOS=%s)", runtime.GOOS)
}

func (*stubDataplane) UpsertService(Service) error {
	return nil
}

func (*stubDataplane) DeleteService(Service) error {
	return nil
}

func (*stubDataplane) CleanupStaleSNATMappings(SNATGCPolicy) (SNATGCResult, error) {
	return SNATGCResult{}, nil
}
