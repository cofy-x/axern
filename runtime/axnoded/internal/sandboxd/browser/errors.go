package browser

import "errors"

var (
	ErrUnavailable     = errors.New("browser provider unavailable")
	ErrInvalidArgument = errors.New("browser invalid argument")
	ErrCommandFailed   = errors.New("browser command failed")
)
