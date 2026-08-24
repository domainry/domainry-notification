package notification

import "errors"

// ErrorKind describes domain failure semantics without binding the module to
// HTTP status codes or a host error package.
type ErrorKind string

const (
	ErrorInvalid     ErrorKind = "invalid"
	ErrorNotFound    ErrorKind = "not_found"
	ErrorConflict    ErrorKind = "conflict"
	ErrorForbidden   ErrorKind = "forbidden"
	ErrorUnavailable ErrorKind = "unavailable"
	ErrorInternal    ErrorKind = "internal"
)

// Error is a stable notification failure. Code remains compatible with the
// existing Plane API; a host maps Kind to its transport-specific status.
type Error struct {
	Kind   ErrorKind
	Code   string
	Params map[string]any
	Err    error
}

func (e *Error) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.Err != nil {
		return e.Code + ": " + e.Err.Error()
	}
	return e.Code
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func NewError(kind ErrorKind, code string, cause error, params map[string]any) error {
	return &Error{Kind: kind, Code: code, Params: cloneParams(params), Err: cause}
}

func ErrorCode(err error) string {
	var value *Error
	if errors.As(err, &value) {
		return value.Code
	}
	return ""
}

func ErrorKindOf(err error) ErrorKind {
	var value *Error
	if errors.As(err, &value) {
		return value.Kind
	}
	return ""
}

func cloneParams(source map[string]any) map[string]any {
	if len(source) == 0 {
		return nil
	}
	result := make(map[string]any, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
