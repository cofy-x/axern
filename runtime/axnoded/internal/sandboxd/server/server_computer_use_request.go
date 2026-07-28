package server

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/computeruse"
)

func screenshotRequestFromQuery(r *http.Request) (computeruse.ScreenshotRequest, error) {
	query := r.URL.Query()
	quality, err := parseOptionalInt(query.Get("quality"))
	if err != nil {
		return computeruse.ScreenshotRequest{}, fmt.Errorf("%w: invalid screenshot quality", computeruse.ErrInvalidArgument)
	}
	scale, err := parseOptionalFloat(query.Get("scale"))
	if err != nil {
		return computeruse.ScreenshotRequest{}, fmt.Errorf("%w: invalid screenshot scale", computeruse.ErrInvalidArgument)
	}
	showCursor, err := parseOptionalBool(query.Get("showCursor"))
	if err != nil {
		return computeruse.ScreenshotRequest{}, fmt.Errorf("%w: invalid showCursor", computeruse.ErrInvalidArgument)
	}
	request := computeruse.ScreenshotRequest{
		ShowCursor: showCursor,
		Format:     query.Get("format"),
		Quality:    quality,
		Scale:      scale,
	}
	x, y := strings.TrimSpace(query.Get("x")), strings.TrimSpace(query.Get("y"))
	width, height := strings.TrimSpace(query.Get("width")), strings.TrimSpace(query.Get("height"))
	if x != "" || y != "" || width != "" || height != "" {
		region, err := parseRegion(x, y, width, height)
		if err != nil {
			return computeruse.ScreenshotRequest{}, err
		}
		request.Region = &region
	}
	return request, nil
}

func parseRegion(x string, y string, width string, height string) (computeruse.Region, error) {
	region := computeruse.Region{}
	var err error
	if region.X, err = parseRequiredInt(x, "region x"); err != nil {
		return computeruse.Region{}, err
	}
	if region.Y, err = parseRequiredInt(y, "region y"); err != nil {
		return computeruse.Region{}, err
	}
	if region.Width, err = parseRequiredInt(width, "region width"); err != nil {
		return computeruse.Region{}, err
	}
	if region.Height, err = parseRequiredInt(height, "region height"); err != nil {
		return computeruse.Region{}, err
	}
	return region, nil
}

func parseRequiredInt(value string, name string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, fmt.Errorf("%w: missing screenshot %s", computeruse.ErrInvalidArgument, name)
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, fmt.Errorf("%w: invalid screenshot %s", computeruse.ErrInvalidArgument, name)
	}
	return parsed, nil
}

func parseOptionalBool(value string) (bool, error) {
	if strings.TrimSpace(value) == "" {
		return false, nil
	}
	parsed, err := strconv.ParseBool(strings.TrimSpace(value))
	if err != nil {
		return false, err
	}
	return parsed, nil
}

func parseOptionalInt(value string) (int, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil {
		return 0, err
	}
	return parsed, nil
}

func parseOptionalFloat(value string) (float64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil {
		return 0, err
	}
	return parsed, nil
}
