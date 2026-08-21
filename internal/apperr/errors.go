package apperr

import (
	"errors"
	"fmt"
)

type Code string

const (
	CodeInvalid         Code = "invalid_argument"
	CodeUnauthenticated Code = "unauthenticated"
	CodeForbidden       Code = "forbidden"
	CodeNotFound        Code = "not_found"
	CodeConflict        Code = "conflict"
	CodeUnavailable     Code = "unavailable"
	CodeInternal        Code = "internal"
)

type Error struct {
	Code    Code
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause == nil {
		return e.Message
	}
	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}
func (e *Error) Unwrap() error { return e.Cause }
func (e *Error) Is(target error) bool {
	other, ok := target.(*Error)
	return ok && other.Code == e.Code
}
func New(code Code, message string) error { return &Error{Code: code, Message: message} }
func Wrap(code Code, message string, cause error) error {
	return &Error{Code: code, Message: message, Cause: cause}
}
func CodeOf(err error) Code {
	var target *Error
	if errors.As(err, &target) {
		return target.Code
	}
	return CodeInternal
}
func IsCode(err error, code Code) bool { return CodeOf(err) == code }
