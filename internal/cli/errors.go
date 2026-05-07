package cli

// Error codes per CLI contract.
const (
	ErrInputTooLarge  = "INPUT_TOO_LARGE"
	ErrInvalidInput   = "INVALID_INPUT"
	ErrUnknownCommand = "UNKNOWN_COMMAND"
	ErrInvalidParams  = "INVALID_PARAMS"
	ErrNoConfig       = "NO_CONFIG"
	ErrInvalidConfig  = "INVALID_CONFIG"
)

// ErrorResponse creates an error response with exit code 2.
func ErrorResponse(code, message string) *Response {
	return &Response{
		Version: ResponseVersion,
		Error:   &ErrorDetail{Code: code, Message: message},
	}
}

// ErrorResponseWithHint creates an error response with a hint.
func ErrorResponseWithHint(code, message, hint string) *Response {
	return &Response{
		Version: ResponseVersion,
		Error:   &ErrorDetail{Code: code, Message: message, Hint: hint},
	}
}
