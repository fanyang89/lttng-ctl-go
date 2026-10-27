//go:build linux && cgo

package lttng

/*
#cgo pkg-config: lttng-ctl
#include <stdlib.h>
#include <lttng/lttng.h>
*/
import "C"

import (
	"context"
	"strings"
	"unsafe"
)

// A channel, rather than sync.Mutex, allows canceled waiters to leave promptly.
var ctlGate = make(chan struct{}, 1)

func lock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case ctlGate <- struct{}{}:
		if err := ctx.Err(); err != nil {
			unlock()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func unlock() { <-ctlGate }

// nativeError must be called while holding ctlGate. Legacy APIs return negative
// error codes; APIs returning enum lttng_error_code are normalized by callers.
func nativeError(op string, result C.int) error {
	if result >= 0 {
		return nil
	}
	return &Error{Op: op, Code: ErrorCode(-result), Message: C.GoString(C.lttng_strerror(result))}
}

func checkString(op, field, value string, limit int, allowEmpty bool) error {
	if !allowEmpty && value == "" {
		return invalid(op, field+" must not be empty")
	}
	if strings.IndexByte(value, 0) >= 0 {
		return invalid(op, field+" contains a NUL byte")
	}
	if limit > 0 && len(value) >= limit {
		return invalid(op, field+" is too long")
	}
	return nil
}

func optionalString(value string) *C.char {
	if value == "" {
		return nil
	}
	return C.CString(value)
}

func free(pointer *C.char) { C.free(unsafe.Pointer(pointer)) }

// copyName is only used on zero-initialized C arrays after length validation.
func copyName(dst []C.char, value string) {
	for i := range len(value) {
		dst[i] = C.char(value[i])
	}
}

func sessionCommand(ctx context.Context, op, session string, call func(*C.char) C.int) error {
	if err := checkString(op, "session name", session, C.LTTNG_NAME_MAX, false); err != nil {
		return err
	}
	if err := lock(ctx); err != nil {
		return err
	}
	defer unlock()
	name := C.CString(session)
	defer free(name)
	return nativeError(op, call(name))
}

func withHandle(ctx context.Context, op, session string, domain Domain, call func(*C.struct_lttng_handle) error) error {
	if err := checkString(op, "session name", session, C.LTTNG_NAME_MAX, false); err != nil {
		return err
	}
	if err := domain.validate(op); err != nil {
		return err
	}
	if err := lock(ctx); err != nil {
		return err
	}
	defer unlock()
	return useHandle(op, session, domain, call)
}

// useHandle requires ctlGate. An empty session is used only for discovery.
func useHandle(op, session string, domain Domain, call func(*C.struct_lttng_handle) error) error {
	name := optionalString(session)
	defer free(name)
	nativeDomain := domain.native()
	handle := C.lttng_create_handle(name, &nativeDomain)
	if handle == nil {
		return nativeError(op, -C.LTTNG_ERR_NOMEM)
	}
	defer C.lttng_destroy_handle(handle)
	return call(handle)
}
