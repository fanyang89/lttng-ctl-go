//go:build linux && cgo

package ctl

/*
#include <stdlib.h>
#include "../internal/ctlstate/bridge_generated.h"
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"github.com/fanyang89/lttng-ctl-go/internal/ctlstate"
)

var (
	// ErrUnsupported identifies APIs absent from the runtime library.
	ErrUnsupported = ctlstate.ErrUnsupported
	// ErrLibraryUnavailable identifies dynamic-loader failures.
	ErrLibraryUnavailable = ctlstate.ErrLibraryUnavailable
	ErrClosed             = errors.New("ctl: closed, moved, or invalidated object")
	ErrReadOnly           = errors.New("ctl: borrowed object is read-only")
	ErrInvalidArgument    = errors.New("ctl: invalid argument")
	ErrNull               = errors.New("ctl: native API returned NULL")
)

// UnsupportedError identifies the missing native symbol.
type UnsupportedError = ctlstate.UnsupportedError

// HasSymbol reports whether the loaded library exports a public API symbol.
// This checks library availability, not daemon, tracer, or permission support.
func HasSymbol(ctx context.Context, symbol string) (bool, error) {
	if err := ctlstate.Lock(ctx); err != nil {
		return false, err
	}
	defer ctlstate.Unlock()
	if !strings.HasPrefix(symbol, "lttng_") || strings.ContainsRune(symbol, 0) {
		return false, ErrInvalidArgument
	}
	return ctlstate.HasSymbol(symbol)
}

// StatusError preserves the status namespace; equal numbers from unrelated
// status enums are not interchangeable. General error messages use strerror.
type StatusError struct {
	Op, Family string
	Code       int
	Message    string
}

func (err *StatusError) Error() string {
	return fmt.Sprintf("ctl: %s: %s=%d: %s", err.Op, err.Family, err.Code, err.Message)
}

func statusError(op, family string, code int, general bool) error {
	if general {
		if code == int(C.LTTNG_OK) {
			return nil
		}
		return &StatusError{Op: op, Family: family, Code: code, Message: C.GoString(C.ctlgo_lttng_strerror(C.int(code)))}
	}
	if code < 0 {
		return &StatusError{Op: op, Family: family, Code: code, Message: "native operation failed"}
	}
	return nil
}

// resource is accessed exclusively under ctlstate.Lock, including its parent.
type resource struct {
	ptr            unsafe.Pointer
	parent         *resource
	parentRevision uint64
	revision       uint64
	readOnly       bool
	dispose        func(unsafe.Pointer)
	extra          []unsafe.Pointer
	closed         bool
	pins           int
	retained       []*resource
}

func (ref *resource) check(write bool) error {
	if ref == nil || ref.ptr == nil || ref.closed {
		return ErrClosed
	}
	if ref.parent != nil {
		if err := ref.parent.check(false); err != nil {
			return err
		}
		if ref.parent.revision != ref.parentRevision {
			return ErrClosed
		}
	}
	if write && ref.readOnly {
		return ErrReadOnly
	}
	return nil
}

func own(ptr unsafe.Pointer, dispose func(unsafe.Pointer)) *resource {
	return &resource{ptr: ptr, dispose: dispose}
}

func borrow(ptr unsafe.Pointer, parent *resource, readOnly bool) *resource {
	ref := &resource{ptr: ptr, parent: parent, readOnly: readOnly}
	if parent != nil {
		ref.parentRevision = parent.revision
	}
	return ref
}

func (ref *resource) close() {
	if ref == nil || ref.ptr == nil || ref.closed {
		return
	}
	ref.closed = true
	ref.revision++
	ref.releaseIfClosed()
}

// Before 2.13 triggers borrow their condition and action without native reference
// counting. Pin the ultimate owner, including when an input is itself borrowed.
// Explicit Close still invalidates its Go wrapper immediately.
func (ref *resource) retain() *resource {
	for ref.parent != nil {
		ref = ref.parent
	}
	ref.pins++
	return ref
}

func (ref *resource) releaseIfClosed() {
	if !ref.closed || ref.pins != 0 || ref.ptr == nil {
		return
	}
	if ref.parent == nil && ref.dispose != nil {
		ref.dispose(ref.ptr)
		for _, p := range ref.extra {
			C.free(p)
		}
	}
	ref.ptr = nil
	for _, retained := range ref.retained {
		retained.pins--
		retained.releaseIfClosed()
	}
	ref.retained = nil
}

func (ref *resource) moved() { ref.ptr = nil; ref.revision++ }

func cstring(value *string) (*C.char, error) {
	if value == nil {
		return nil, nil
	}
	if strings.IndexByte(*value, 0) >= 0 {
		return nil, fmt.Errorf("%w: embedded NUL", ErrInvalidArgument)
	}
	return C.CString(*value), nil
}

// List owns a native array. Items returns a Go slice of borrowed typed objects;
// the objects become invalid after Close. Closing an individual item is safe.
type List[T any] struct {
	ref   *resource
	items []T
}

func (list *List[T]) Items() []T {
	if list == nil {
		return nil
	}
	return append([]T(nil), list.items...)
}

func (list *List[T]) Close() error {
	if err := ctlstate.Lock(context.Background()); err != nil {
		return err
	}
	defer ctlstate.Unlock()
	if list != nil {
		list.ref.close()
	}
	return nil
}
