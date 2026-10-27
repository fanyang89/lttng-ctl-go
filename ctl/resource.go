//go:build linux && cgo

package ctl

/*
#cgo pkg-config: lttng-ctl
#include <stdlib.h>
#include <lttng/lttng.h>
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unsafe"

	"github.com/fanyang89/lttng-go/internal/ctlstate"
)

var (
	ErrClosed          = errors.New("ctl: closed, moved, or invalidated object")
	ErrReadOnly        = errors.New("ctl: borrowed object is read-only")
	ErrInvalidArgument = errors.New("ctl: invalid argument")
	ErrNull            = errors.New("ctl: native API returned NULL")
)

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
		return &StatusError{Op: op, Family: family, Code: code, Message: C.GoString(C.lttng_strerror(C.int(code)))}
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
}

func (ref *resource) check(write bool) error {
	if ref == nil || ref.ptr == nil {
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
	if ref == nil || ref.ptr == nil {
		return
	}
	if ref.parent == nil && ref.dispose != nil {
		ref.dispose(ref.ptr)
		for _, p := range ref.extra {
			C.free(p)
		}
	}
	ref.ptr = nil
	ref.revision++
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
