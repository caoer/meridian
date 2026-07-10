package cli

// Error codes per CLI contract.
const (
	ErrNoInput           = "NO_INPUT"
	ErrInputTooLarge     = "INPUT_TOO_LARGE"
	ErrInvalidInput      = "INVALID_INPUT"
	ErrUnknownCommand    = "UNKNOWN_COMMAND"
	ErrInvalidParams     = "INVALID_PARAMS"
	ErrNoConfig          = "NO_CONFIG"
	ErrInvalidConfig     = "INVALID_CONFIG"
	ErrInvalidConfigPath = "INVALID_CONFIG_PATH"
	ErrRulePackNotFound  = "RULE_PACK_NOT_FOUND"
	ErrInvalidRule       = "INVALID_RULE"
	ErrDuplicateRule     = "DUPLICATE_RULE"
	ErrInvalidGlob       = "INVALID_GLOB"
	ErrInvalidTemplate   = "INVALID_TEMPLATE"
	ErrYAMLDepthExceeded = "YAML_DEPTH_EXCEEDED"
	ErrWatchFailed       = "WATCH_FAILED"
	ErrNoDaemon          = "NO_DAEMON"
	ErrStatusFailed      = "STATUS_FAILED"
	ErrAmbiguousTarget   = "AMBIGUOUS_TARGET"
	ErrWrongCwd          = "WRONG_CWD"
	ErrCacheFailed       = "CACHE_FAILED"
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
