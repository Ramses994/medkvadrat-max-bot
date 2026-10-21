package gateway

import (
	"errors"
	"fmt"
	"net/url"
)

// Error is a gateway failure without request URL or phone in the message.
type Error struct {
	Op     string
	Status int
	Code   string
	Err    error
}

func (e *Error) Error() string {
	msg := "gateway " + e.Op
	if e.Status != 0 {
		msg += fmt.Sprintf(" %d", e.Status)
	}
	if e.Code != "" {
		msg += " " + e.Code
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *Error) Unwrap() error { return e.Err }

func wrapNet(op string, err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return &Error{Op: op, Err: ue.Err}
	}
	return &Error{Op: op, Err: err}
}
