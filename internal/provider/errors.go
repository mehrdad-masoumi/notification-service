package providerrerrors

import (
	"errors"
	"fmt"
)

type TemporaryError struct {
	Msg string
	Err error
}

func (e *TemporaryError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Err)
	}
	return e.Msg
}

func (e *TemporaryError) Unwrap() error { return e.Err }

func Temporary(msg string, err error) error {
	return &TemporaryError{Msg: msg, Err: err}
}

type PermanentError struct {
	Msg string
	Err error
}

func (e *PermanentError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Msg, e.Err)
	}
	return e.Msg
}

func (e *PermanentError) Unwrap() error { return e.Err }

func Permanent(msg string, err error) error {
	return &PermanentError{Msg: msg, Err: err}
}

func IsTemporary(err error) bool {
	var te *TemporaryError
	return errors.As(err, &te)
}

func IsPermanent(err error) bool {
	var pe *PermanentError
	return errors.As(err, &pe)
}
