//go:build linux && cgo

package ctlstate

/*
#cgo LDFLAGS: -ldl
#include <stdlib.h>
#include "loader.h"
*/
import "C"

import (
	"errors"
	"fmt"
	"unsafe"
)

var (
	ErrUnsupported        = errors.New("lttng: native API is unavailable")
	ErrLibraryUnavailable = errors.New("lttng: cannot load liblttng-ctl")
)

// UnsupportedError identifies an API absent from the loaded control library.
type UnsupportedError struct{ Symbol string }

func (err *UnsupportedError) Error() string { return fmt.Sprintf("%s: %s", ErrUnsupported, err.Symbol) }
func (err *UnsupportedError) Unwrap() error { return ErrUnsupported }

// HasSymbol requires Lock. It does not set the per-call missing-symbol error.
func HasSymbol(name string) (bool, error) {
	native := C.CString(name)
	defer C.free(unsafe.Pointer(native))
	found := C.ctlgo_lookup(native) != nil
	if message := C.ctlgo_load_error(); message != nil {
		return false, fmt.Errorf("%w: %s", ErrLibraryUnavailable, C.GoString(message))
	}
	return found, nil
}

// Require checks all symbols before a native operation, including any destructor
// needed to release its result. The caller must hold Lock.
func Require(names ...string) error {
	for _, name := range names {
		found, err := HasSymbol(name)
		if err != nil {
			return err
		}
		if !found {
			return &UnsupportedError{Symbol: name}
		}
	}
	return nil
}

// CallError reports a bridge failure before interpreting a native return value.
// The error stays available until the next Lock, including across cleanup calls.
func CallError() error {
	symbol := C.ctlgo_missing_symbol()
	if symbol == nil {
		return nil
	}
	if message := C.ctlgo_load_error(); message != nil {
		return fmt.Errorf("%w: %s", ErrLibraryUnavailable, C.GoString(message))
	}
	return &UnsupportedError{Symbol: C.GoString(symbol)}
}

func resetError() { C.ctlgo_reset_error() }
