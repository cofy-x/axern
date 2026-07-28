package wire

const (
	ErrorCodeAlreadyExists    = "already_exists"
	ErrorCodeCommandFailed    = "command_failed"
	ErrorCodeFailedCondition  = "failed_precondition"
	ErrorCodeInternal         = "internal_error"
	ErrorCodeInvalidArgument  = "invalid_argument"
	ErrorCodeMethodNotAllowed = "method_not_allowed"
	ErrorCodeNotFound         = "not_found"
	ErrorCodeTimeout          = "timeout"
	ErrorCodeUnavailable      = "unavailable"
)

type ErrorResponse struct {
	Error ResponseError `json:"error"`
}

type ResponseError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}
