package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/cofy-x/axern/runtime/axnoded/internal/sandboxd/wire"
)

const maxJSONRequestBytes = 1 << 20

const (
	errorCodeAlreadyExists    = wire.ErrorCodeAlreadyExists
	errorCodeCommandFailed    = wire.ErrorCodeCommandFailed
	errorCodeFailedCondition  = wire.ErrorCodeFailedCondition
	errorCodeInternal         = wire.ErrorCodeInternal
	errorCodeInvalidArgument  = wire.ErrorCodeInvalidArgument
	errorCodeMethodNotAllowed = wire.ErrorCodeMethodNotAllowed
	errorCodeNotFound         = wire.ErrorCodeNotFound
	errorCodeTimeout          = wire.ErrorCodeTimeout
	errorCodeUnavailable      = wire.ErrorCodeUnavailable
)

func decodeOptionalJSONRequest(r *http.Request, target any) error {
	return decodeJSONRequest(r, target, false)
}

func decodeRequiredJSONRequest(r *http.Request, target any) error {
	return decodeJSONRequest(r, target, true)
}

func decodeJSONRequest(r *http.Request, target any, required bool) error {
	reader := io.LimitReader(r.Body, maxJSONRequestBytes+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return err
	}
	if len(data) > maxJSONRequestBytes {
		return fmt.Errorf("request body exceeds %d bytes", maxJSONRequestBytes)
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		if required {
			return io.EOF
		}
		return nil
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("request body contains trailing JSON")
	}
	return nil
}

func writeProviderError(w http.ResponseWriter, err error, unavailable error, invalidArgument error) {
	status := http.StatusInternalServerError
	code := errorCodeInternal
	if errors.Is(err, unavailable) {
		status = http.StatusServiceUnavailable
		code = errorCodeUnavailable
	} else if errors.Is(err, invalidArgument) {
		status = http.StatusBadRequest
		code = errorCodeInvalidArgument
	}
	writeError(w, status, code, err.Error())
}

func writeError(w http.ResponseWriter, status int, code string, message string) {
	if message == "" {
		message = http.StatusText(status)
	}
	if code == "" {
		code = errorCodeForStatus(status)
	}
	writeJSON(w, status, wire.ErrorResponse{Error: wire.ResponseError{Code: code, Message: message}})
}

func writeMethodNotAllowed(w http.ResponseWriter) {
	writeError(w, http.StatusMethodNotAllowed, errorCodeMethodNotAllowed, "method not allowed")
}

func writeNotFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, errorCodeNotFound, "not found")
}

func errorCodeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return errorCodeInvalidArgument
	case http.StatusNotFound:
		return errorCodeNotFound
	case http.StatusConflict:
		return errorCodeAlreadyExists
	case http.StatusMethodNotAllowed:
		return errorCodeMethodNotAllowed
	case http.StatusRequestTimeout:
		return errorCodeTimeout
	case http.StatusPreconditionFailed:
		return errorCodeFailedCondition
	case http.StatusServiceUnavailable:
		return errorCodeUnavailable
	default:
		return errorCodeInternal
	}
}
