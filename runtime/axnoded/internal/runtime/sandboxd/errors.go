package sandboxd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
	"github.com/cofy-x/axern/runtime/axnoded/pkg/errord"
)

type StatusError struct {
	Path       string
	StatusCode int
	Code       string
	Message    string
}

const (
	ErrorCodeAlreadyExists    = wire.ErrorCodeAlreadyExists
	ErrorCodeCommandFailed    = wire.ErrorCodeCommandFailed
	ErrorCodeFailedCondition  = wire.ErrorCodeFailedCondition
	ErrorCodeInternal         = wire.ErrorCodeInternal
	ErrorCodeInvalidArgument  = wire.ErrorCodeInvalidArgument
	ErrorCodeMethodNotAllowed = wire.ErrorCodeMethodNotAllowed
	ErrorCodeNotFound         = wire.ErrorCodeNotFound
	ErrorCodeTimeout          = wire.ErrorCodeTimeout
	ErrorCodeUnavailable      = wire.ErrorCodeUnavailable
)

type ErrorClass string

const (
	ErrorClassInvalidArgument  ErrorClass = "invalid_argument"
	ErrorClassNotFound         ErrorClass = "not_found"
	ErrorClassAlreadyExists    ErrorClass = "already_exists"
	ErrorClassFailedCondition  ErrorClass = "failed_precondition"
	ErrorClassUnavailable      ErrorClass = "unavailable"
	ErrorClassCommandFailed    ErrorClass = "command_failed"
	ErrorClassTimeout          ErrorClass = "timeout"
	ErrorClassMethodNotAllowed ErrorClass = "method_not_allowed"
	ErrorClassInternal         ErrorClass = "internal_error"
)

func (e *StatusError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("sandboxd %s returned status %d", e.Path, e.StatusCode)
	}
	if e.Code != "" {
		return fmt.Sprintf("sandboxd %s returned status %d (%s): %s", e.Path, e.StatusCode, e.Code, e.Message)
	}
	return fmt.Sprintf("sandboxd %s returned status %d: %s", e.Path, e.StatusCode, e.Message)
}

func StatusCode(err error) int {
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		return statusErr.StatusCode
	}
	return 0
}

func ErrorCode(err error) string {
	var statusErr *StatusError
	if errors.As(err, &statusErr) {
		return statusErr.Code
	}
	return ""
}

func ClassifyError(err error) ErrorClass {
	switch ErrorCode(err) {
	case ErrorCodeInvalidArgument:
		return ErrorClassInvalidArgument
	case ErrorCodeNotFound:
		return ErrorClassNotFound
	case ErrorCodeAlreadyExists:
		return ErrorClassAlreadyExists
	case ErrorCodeUnavailable:
		return ErrorClassUnavailable
	case ErrorCodeCommandFailed:
		return ErrorClassCommandFailed
	case ErrorCodeTimeout:
		return ErrorClassTimeout
	case ErrorCodeMethodNotAllowed:
		return ErrorClassMethodNotAllowed
	case ErrorCodeFailedCondition:
		return ErrorClassFailedCondition
	case ErrorCodeInternal:
		return ErrorClassInternal
	}
	switch StatusCode(err) {
	case http.StatusBadRequest:
		return ErrorClassInvalidArgument
	case http.StatusNotFound:
		return ErrorClassNotFound
	case http.StatusConflict:
		return ErrorClassAlreadyExists
	case http.StatusServiceUnavailable:
		return ErrorClassUnavailable
	case http.StatusRequestTimeout:
		return ErrorClassTimeout
	case http.StatusMethodNotAllowed:
		return ErrorClassMethodNotAllowed
	case http.StatusInternalServerError:
		return ErrorClassInternal
	default:
		return ErrorClassFailedCondition
	}
}

func OperationError(capability string, operation string, err error) error {
	return ResourceOperationError(capability, operation, "", err)
}

func ResourceOperationError(capability string, operation string, resource string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	prefix := "sandboxd"
	if capability = strings.TrimSpace(capability); capability != "" {
		prefix += " " + capability
	}
	if operation = strings.TrimSpace(operation); operation != "" {
		prefix += " " + operation
	}
	if resource = strings.TrimSpace(resource); resource != "" {
		prefix += " " + resource
	}
	return fmt.Errorf("%s failed: %v: %w", prefix, err, errorClassStatus(ClassifyError(err)))
}

func errorClassStatus(class ErrorClass) error {
	switch class {
	case ErrorClassInvalidArgument:
		return errord.ErrInvalidArgument
	case ErrorClassNotFound:
		return errord.ErrNotFound
	case ErrorClassAlreadyExists:
		return errord.ErrAlreadyExists
	case ErrorClassUnavailable,
		ErrorClassCommandFailed,
		ErrorClassTimeout,
		ErrorClassMethodNotAllowed,
		ErrorClassInternal,
		ErrorClassFailedCondition:
		return errord.ErrFailedPrecondition
	default:
		return errord.ErrFailedPrecondition
	}
}

func sandboxdStatusError(path string, resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	message := strings.TrimSpace(string(body))
	code, parsedMessage := parseErrorResponse(body)
	if parsedMessage != "" {
		message = parsedMessage
	}
	err := &StatusError{Path: path, StatusCode: resp.StatusCode, Code: code, Message: message}
	if message == "" {
		err.Message = http.StatusText(resp.StatusCode)
	}
	return err
}

func parseErrorResponse(body []byte) (string, string) {
	var response struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", ""
	}
	return response.Error.Code, response.Error.Message
}
