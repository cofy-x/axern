package computeruse

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func (s *Service) Screenshot(ctx context.Context, request ScreenshotRequest) (ScreenshotResponse, error) {
	if err := s.requireAvailable(); err != nil {
		return ScreenshotResponse{}, err
	}
	format, contentType, err := screenshotFormat(request.Format)
	if err != nil {
		return ScreenshotResponse{}, err
	}
	if command := strings.TrimSpace(os.Getenv("AXERN_SANDBOXD_SCREENSHOT_CMD")); command != "" {
		data, err := runShellOutput(ctx, s.waiter, command, screenshotEnv(request))
		return ScreenshotResponse{Data: data, ContentType: contentType}, err
	}
	if request.ShowCursor {
		return ScreenshotResponse{}, fmt.Errorf("%w: showCursor is not supported by the x11 screenshot backend", ErrInvalidArgument)
	}
	args := []string{"-window", "root"}
	if s.display != "" {
		args = append([]string{"-display", s.display}, args...)
	}
	if request.Region != nil {
		if err := validateRegion(*request.Region); err != nil {
			return ScreenshotResponse{}, err
		}
		args = append(args, "-crop", fmt.Sprintf("%dx%d+%d+%d", request.Region.Width, request.Region.Height, request.Region.X, request.Region.Y))
	}
	if request.Quality > 0 {
		if request.Quality > 100 {
			return ScreenshotResponse{}, fmt.Errorf("%w: screenshot quality must be between 1 and 100", ErrInvalidArgument)
		}
		args = append(args, "-quality", strconv.Itoa(request.Quality))
	}
	if request.Scale > 0 {
		if request.Scale > 4 {
			return ScreenshotResponse{}, fmt.Errorf("%w: screenshot scale must be greater than 0 and less than or equal to 4", ErrInvalidArgument)
		}
		args = append(args, "-resize", fmt.Sprintf("%d%%", int(request.Scale*100)))
	}
	args = append(args, format+":-")
	data, err := runCommandOutput(ctx, s.waiter, "import", args, screenshotEnv(request))
	return ScreenshotResponse{Data: data, ContentType: contentType}, err
}

func screenshotFormat(value string) (string, string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "png":
		return "png", "image/png", nil
	case "jpeg", "jpg":
		return "jpeg", "image/jpeg", nil
	default:
		return "", "", fmt.Errorf("%w: unsupported screenshot format %q", ErrInvalidArgument, value)
	}
}

func validateRegion(region Region) error {
	if region.Width <= 0 || region.Height <= 0 {
		return fmt.Errorf("%w: screenshot region width and height must be positive", ErrInvalidArgument)
	}
	if region.X < 0 || region.Y < 0 {
		return fmt.Errorf("%w: screenshot region x and y must be non-negative", ErrInvalidArgument)
	}
	return nil
}

func screenshotEnv(request ScreenshotRequest) []string {
	env := []string{
		"AXERN_COMPUTER_USE_SCREENSHOT_SHOW_CURSOR=" + strconv.FormatBool(request.ShowCursor),
		"AXERN_COMPUTER_USE_SCREENSHOT_FORMAT=" + strings.TrimSpace(request.Format),
		"AXERN_COMPUTER_USE_SCREENSHOT_QUALITY=" + strconv.Itoa(request.Quality),
		"AXERN_COMPUTER_USE_SCREENSHOT_SCALE=" + strconv.FormatFloat(request.Scale, 'f', -1, 64),
	}
	if request.Region != nil {
		env = append(env,
			"AXERN_COMPUTER_USE_SCREENSHOT_X="+strconv.Itoa(request.Region.X),
			"AXERN_COMPUTER_USE_SCREENSHOT_Y="+strconv.Itoa(request.Region.Y),
			"AXERN_COMPUTER_USE_SCREENSHOT_WIDTH="+strconv.Itoa(request.Region.Width),
			"AXERN_COMPUTER_USE_SCREENSHOT_HEIGHT="+strconv.Itoa(request.Region.Height),
		)
	}
	return env
}
