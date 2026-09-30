package route

import (
	"errors"
	"fmt"
	"net/http"
)

// Code is the public failure class written to the client.
type Code int

const (
	CodeCanceled Code = iota + 1
	CodeUnknown
	CodeInvalidArgument
	CodeDeadlineExceeded
	CodeNotFound
	CodeAlreadyExists
	CodePermissionDenied
	CodeResourceExhausted
	CodeFailedPrecondition
	CodeAborted
	CodeOutOfRange
	CodeUnimplemented
	CodeInternal
	CodeUnavailable
	CodeUnauthenticated
)

// String returns the stable snake_case code the client matches on.
func (c Code) String() string {
	switch c {
	case CodeCanceled:
		return "canceled"
	case CodeUnknown:
		return "unknown"
	case CodeInvalidArgument:
		return "invalid_argument"
	case CodeDeadlineExceeded:
		return "deadline_exceeded"
	case CodeNotFound:
		return "not_found"
	case CodeAlreadyExists:
		return "already_exists"
	case CodePermissionDenied:
		return "permission_denied"
	case CodeResourceExhausted:
		return "resource_exhausted"
	case CodeFailedPrecondition:
		return "failed_precondition"
	case CodeAborted:
		return "aborted"
	case CodeOutOfRange:
		return "out_of_range"
	case CodeUnimplemented:
		return "unimplemented"
	case CodeInternal:
		return "internal"
	case CodeUnavailable:
		return "unavailable"
	case CodeUnauthenticated:
		return "unauthenticated"
	default:
		return "unknown"
	}
}

// StatusCode is the HTTP status used for this code.
func (c Code) StatusCode() int {
	switch c {
	case CodeInvalidArgument, CodeOutOfRange:
		return http.StatusBadRequest
	case CodeUnauthenticated:
		return http.StatusUnauthorized
	case CodePermissionDenied:
		return http.StatusForbidden
	case CodeNotFound:
		return http.StatusNotFound
	case CodeAlreadyExists, CodeAborted:
		return http.StatusConflict
	case CodeFailedPrecondition:
		return http.StatusPreconditionFailed
	case CodeResourceExhausted:
		return http.StatusRequestEntityTooLarge
	case CodeCanceled:
		return 499
	case CodeUnimplemented:
		return http.StatusMethodNotAllowed
	case CodeUnavailable:
		return http.StatusServiceUnavailable
	case CodeDeadlineExceeded:
		return http.StatusGatewayTimeout
	default:
		return http.StatusInternalServerError
	}
}

// Error is a failure the client is allowed to see.
// The Message is public. The wrapped cause is for [Server.OnError] only.
type Error struct {
	Code    Code
	Message string
	err     error
}

// NewError returns a public failure. message is sent to the client as-is.
func NewError(code Code, message string) error {
	return &Error{Code: code, Message: message}
}

// WrapError returns a public failure that keeps cause for logs.
// cause is not written to the response.
func WrapError(code Code, message string, cause error) error {
	return &Error{Code: code, Message: message, err: cause}
}

// Error returns the public message. A wrapped cause is appended for logs.
func (e *Error) Error() string {
	if e.err == nil {
		return e.Message
	}
	return e.Message + ": " + e.err.Error()
}

// Unwrap returns the cause passed to WrapError.
func (e *Error) Unwrap() error { return e.err }

// Public splits err into the code and message safe to send to the client.
// Unclassified errors become an internal error with a fixed message.
func Public(err error) (Code, string) {
	var se *Error
	if errors.As(err, &se) && se.Message != "" {
		return se.Code, se.Message
	}
	return CodeInternal, "internal error"
}

// Errorf formats a public failure. The format is the client message.
func Errorf(code Code, format string, args ...any) error {
	return NewError(code, fmt.Sprintf(format, args...))
}
