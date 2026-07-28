package computeruse

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func (s *Service) Display(ctx context.Context) (DisplayResponse, error) {
	if err := s.requireAvailable(); err != nil {
		return DisplayResponse{}, err
	}
	resp := DisplayResponse{Display: s.display, Backend: s.backend}
	var out []byte
	var err error
	if command := strings.TrimSpace(os.Getenv("AXERN_SANDBOXD_DISPLAY_CMD")); command != "" {
		out, err = runShellOutput(ctx, s.waiter, command, nil)
	} else {
		out, err = runCommandOutput(ctx, s.waiter, "xdotool", []string{"getdisplaygeometry"}, nil)
	}
	if err != nil {
		return resp, err
	}
	width, height, err := parseDisplayGeometry(out)
	if err != nil {
		return resp, err
	}
	resp.Width = width
	resp.Height = height
	return resp, nil
}

func parseDisplayGeometry(out []byte) (int, int, error) {
	fields := strings.Fields(string(out))
	if len(fields) < 2 {
		return 0, 0, fmt.Errorf("%w: invalid display geometry output %q", ErrCommandFailed, strings.TrimSpace(string(out)))
	}
	width, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("%w: invalid display width %q", ErrCommandFailed, fields[0])
	}
	height, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("%w: invalid display height %q", ErrCommandFailed, fields[1])
	}
	if width <= 0 || height <= 0 {
		return 0, 0, fmt.Errorf("%w: display width and height must be positive", ErrCommandFailed)
	}
	return width, height, nil
}
