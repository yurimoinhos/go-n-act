package route

import (
	"errors"
	"fmt"
	"net/http"
)

// Code is the public failure class written to the client as the problem "code" extension.
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

// Title is the short HTTP reason phrase used as the problem title.
func (c Code) Title() string {
	switch c {
	case CodeInvalidArgument, CodeOutOfRange:
		return "Bad Request"
	case CodeUnauthenticated:
		return "Unauthorized"
	case CodePermissionDenied:
		return "Forbidden"
	case CodeNotFound:
		return "Not Found"
	case CodeAlreadyExists, CodeAborted:
		return "Conflict"
	case CodeFailedPrecondition:
		return "Precondition Failed"
	case CodeResourceExhausted:
		return "Request Entity Too Large"
	case CodeCanceled:
		return "Client Closed Request"
	case CodeUnimplemented:
		return "Method Not Allowed"
	case CodeUnavailable:
		return "Service Unavailable"
	case CodeDeadlineExceeded:
		return "Gateway Timeout"
	default:
		return "Internal Server Error"
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

// TypeURI returns the stable problem type URI for this code.
func (c Code) TypeURI() string {
	return "urn:gnact:error:" + c.String()
}

// Problem is an RFC 7807 problem details document exposed to clients.
type Problem struct {
	Type       string         `json:"type"`
	Title      string         `json:"title"`
	Status     int            `json:"status"`
	Detail     string         `json:"detail"`
	Instance   string         `json:"instance,omitempty"`
	Code       string         `json:"code"`
	Extensions map[string]any `json:"-"`
}

// Error is a failure the client is allowed to see.
// The Message becomes the problem detail. The wrapped cause is for [Server.OnError] only.
type Error struct {
	Code       Code
	Message    string
	Type       string
	Instance   string
	Extensions map[string]any
	err        error
}

// NewError returns a public failure. message is sent to the client as the detail.
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

// Problem builds the RFC 7807 document for this error.
func (e *Error) Problem() Problem {
	typ := e.Type
	if typ == "" {
		typ = e.Code.TypeURI()
	}
	return Problem{
		Type:       typ,
		Title:      e.Code.Title(),
		Status:     e.Code.StatusCode(),
		Detail:     e.Message,
		Instance:   e.Instance,
		Code:       e.Code.String(),
		Extensions: e.Extensions,
	}
}

// PublicProblem splits err into a problem safe to send to the client.
// Unclassified errors become an internal problem with a fixed detail.
func PublicProblem(err error) Problem {
	var se *Error
	if errors.As(err, &se) && se.Message != "" {
		return se.Problem()
	}
	return Problem{
		Type:   CodeInternal.TypeURI(),
		Title:  CodeInternal.Title(),
		Status: CodeInternal.StatusCode(),
		Detail: "internal error",
		Code:   CodeInternal.String(),
	}
}

// Public splits err into the code and message safe to send to the client.
// Deprecated: prefer [PublicProblem]. Kept for callers that only need code/message.
func Public(err error) (Code, string) {
	p := PublicProblem(err)
	var se *Error
	if errors.As(err, &se) && se.Message != "" {
		return se.Code, se.Message
	}
	return CodeInternal, p.Detail
}

// Errorf formats a public failure. The format is the client detail.
func Errorf(code Code, format string, args ...any) error {
	return NewError(code, fmt.Sprintf(format, args...))
}
