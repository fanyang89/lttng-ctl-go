//go:build linux && cgo

package lttng

/*
#include <lttng/lttng-error.h>
*/
import "C"

import (
	"fmt"

	"github.com/fanyang89/lttng-ctl-go/internal/ctlstate"
)

// ErrLibraryUnavailable identifies a failure to load the runtime control library.
var ErrLibraryUnavailable = ctlstate.ErrLibraryUnavailable

// ErrorCode is a positive liblttng-ctl error code. It can be used with errors.Is.
type ErrorCode int

// Common liblttng-ctl error codes. Unlisted native codes are preserved as well.
const (
	ErrNoSession       ErrorCode = C.LTTNG_ERR_NO_SESSION
	ErrSessionNotFound ErrorCode = C.LTTNG_ERR_SESS_NOT_FOUND
	ErrSessionExists   ErrorCode = C.LTTNG_ERR_EXIST_SESS
	ErrNoSessiond      ErrorCode = C.LTTNG_ERR_NO_SESSIOND
	ErrPermission      ErrorCode = C.LTTNG_ERR_EPERM
	ErrInvalid         ErrorCode = C.LTTNG_ERR_INVALID
	ErrNoMemory        ErrorCode = C.LTTNG_ERR_NOMEM
	ErrUnsupported     ErrorCode = C.LTTNG_ERR_NOT_SUPPORTED
	ErrAlreadyStarted  ErrorCode = C.LTTNG_ERR_TRACE_ALREADY_STARTED
	ErrAlreadyStopped  ErrorCode = C.LTTNG_ERR_TRACE_ALREADY_STOPPED
	ErrInvalidFilter   ErrorCode = C.LTTNG_ERR_FILTER_INVAL
)

func (code ErrorCode) Error() string {
	return fmt.Sprintf("LTTng error %d", int(code))
}

// Error describes a native library failure. Code retains the upstream code;
// Message is copied from lttng_strerror while the library is locked.
type Error struct {
	Op      string
	Code    ErrorCode
	Message string
}

func (err *Error) Error() string { return "lttng: " + err.Op + ": " + err.Message }

func (err *Error) Unwrap() error { return err.Code }

func invalid(op, reason string) error {
	return fmt.Errorf("lttng: %s: %w: %s", op, ErrInvalid, reason)
}
