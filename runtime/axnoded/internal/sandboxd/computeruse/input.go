package computeruse

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func (s *Service) Mouse(ctx context.Context, request MouseRequest) error {
	if err := s.requireAvailable(); err != nil {
		return err
	}
	action := strings.TrimSpace(request.Action)
	if action == "" {
		action = "click"
	}
	button := strings.TrimSpace(request.Button)
	if button == "" {
		button = "1"
	}
	if err := validateMouseRequest(action, request); err != nil {
		return err
	}
	amount := request.Amount
	if amount <= 0 {
		amount = 1
	}
	env := []string{
		"AXERN_COMPUTER_USE_MOUSE_X=" + strconv.Itoa(request.X),
		"AXERN_COMPUTER_USE_MOUSE_Y=" + strconv.Itoa(request.Y),
		"AXERN_COMPUTER_USE_MOUSE_TO_X=" + strconv.Itoa(request.ToX),
		"AXERN_COMPUTER_USE_MOUSE_TO_Y=" + strconv.Itoa(request.ToY),
		"AXERN_COMPUTER_USE_MOUSE_BUTTON=" + button,
		"AXERN_COMPUTER_USE_MOUSE_ACTION=" + action,
		"AXERN_COMPUTER_USE_MOUSE_DIRECTION=" + strings.TrimSpace(request.Direction),
		"AXERN_COMPUTER_USE_MOUSE_AMOUNT=" + strconv.Itoa(amount),
	}
	if command := strings.TrimSpace(os.Getenv("AXERN_SANDBOXD_MOUSE_CMD")); command != "" {
		_, err := runShellOutput(ctx, s.waiter, command, env)
		return err
	}
	args, err := mouseArgs(action, button, request, amount)
	if err != nil {
		return err
	}
	_, err = runCommandOutput(ctx, s.waiter, "xdotool", args, env)
	return err
}

func (s *Service) Keyboard(ctx context.Context, request KeyboardRequest) error {
	if err := s.requireAvailable(); err != nil {
		return err
	}
	env := []string{
		"AXERN_COMPUTER_USE_KEYBOARD_TEXT=" + request.Text,
		"AXERN_COMPUTER_USE_KEYBOARD_KEY=" + request.Key,
		"AXERN_COMPUTER_USE_KEYBOARD_KEYS=" + strings.Join(request.Keys, "+"),
		"AXERN_COMPUTER_USE_KEYBOARD_DELAY_MS=" + strconv.Itoa(request.DelayMS),
	}
	if command := strings.TrimSpace(os.Getenv("AXERN_SANDBOXD_KEYBOARD_CMD")); command != "" {
		_, err := runShellOutput(ctx, s.waiter, command, env)
		return err
	}
	args, err := keyboardArgs(request)
	if err != nil {
		return err
	}
	_, err = runCommandOutput(ctx, s.waiter, "xdotool", args, env)
	return err
}

func validateMouseRequest(action string, request MouseRequest) error {
	if request.X < 0 || request.Y < 0 {
		return fmt.Errorf("%w: mouse coordinates must be non-negative", ErrInvalidArgument)
	}
	switch action {
	case "move", "click", "double_click", "scroll":
		return nil
	case "drag":
		if request.ToX < 0 || request.ToY < 0 {
			return fmt.Errorf("%w: mouse drag destination must be non-negative", ErrInvalidArgument)
		}
		return nil
	default:
		return nil
	}
}

func mouseArgs(action string, button string, request MouseRequest, amount int) ([]string, error) {
	switch action {
	case "move":
		return []string{"mousemove", strconv.Itoa(request.X), strconv.Itoa(request.Y)}, nil
	case "click":
		return []string{"mousemove", strconv.Itoa(request.X), strconv.Itoa(request.Y), "click", button}, nil
	case "double_click":
		return []string{"mousemove", strconv.Itoa(request.X), strconv.Itoa(request.Y), "click", "--repeat", "2", button}, nil
	case "drag":
		return []string{"mousemove", strconv.Itoa(request.X), strconv.Itoa(request.Y), "mousedown", button, "mousemove", strconv.Itoa(request.ToX), strconv.Itoa(request.ToY), "mouseup", button}, nil
	case "scroll":
		scrollButton, err := scrollButton(request.Direction)
		if err != nil {
			return nil, err
		}
		return []string{"mousemove", strconv.Itoa(request.X), strconv.Itoa(request.Y), "click", "--repeat", strconv.Itoa(amount), scrollButton}, nil
	default:
		return nil, fmt.Errorf("%w: unsupported mouse action %q", ErrInvalidArgument, action)
	}
}

func scrollButton(direction string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "", "down":
		return "5", nil
	case "up":
		return "4", nil
	case "left":
		return "6", nil
	case "right":
		return "7", nil
	default:
		return "", fmt.Errorf("%w: unsupported mouse scroll direction %q", ErrInvalidArgument, direction)
	}
}

func keyboardArgs(request KeyboardRequest) ([]string, error) {
	if len(request.Keys) > 0 {
		return []string{"key", strings.Join(request.Keys, "+")}, nil
	}
	if key := strings.TrimSpace(request.Key); key != "" {
		return []string{"key", key}, nil
	}
	if request.Text == "" {
		return nil, fmt.Errorf("%w: keyboard request requires text, key, or keys", ErrInvalidArgument)
	}
	args := []string{"type"}
	if request.DelayMS > 0 {
		args = append(args, "--delay", strconv.Itoa(request.DelayMS))
	}
	return append(args, request.Text), nil
}
